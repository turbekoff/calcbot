package env

import (
	"encoding"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	TagValue   = "env"
	TagDefault = "env-default"
)

type parseFunc func(*reflect.Value, string) error

var parsers = map[reflect.Type]parseFunc{
	reflect.TypeOf(struct{}{}): func(fieldValue *reflect.Value, env string) error {
		fieldValue.Set(reflect.ValueOf(struct{}{}))
		return nil
	},

	reflect.TypeOf(url.URL{}): func(fieldValue *reflect.Value, env string) error {
		URL, err := url.Parse(env)
		if err != nil {
			return err
		}

		fieldValue.Set(reflect.ValueOf(*URL))
		return nil
	},

	reflect.TypeOf(time.Location{}): func(fieldValue *reflect.Value, env string) error {
		location, err := time.LoadLocation(env)
		if err != nil {
			return err
		}

		fieldValue.Set(reflect.ValueOf(*location))
		return nil
	},
}

func Read(root interface{}) error {
	rootValue := reflect.ValueOf(root)

	if rootValue.Kind() == reflect.Ptr {
		rootValue = rootValue.Elem()
	}

	if rootValue.Kind() != reflect.Struct {
		return fmt.Errorf("unexpected type %v", rootValue.Kind())
	}

	rootType := rootValue.Type()
	for i := 0; i < rootValue.NumField(); i++ {
		fieldType := rootType.Field(i)
		fieldValue := rootValue.Field(i)

		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				fieldValue.Set(reflect.New(fieldType.Type.Elem()))
			}

			fieldValue = fieldValue.Elem()
		}

		if fieldValue.Kind() == reflect.Struct {
			if !fieldValue.CanInterface() {
				continue
			}

			err := Read(fieldValue.Addr().Interface())
			if err != nil {
				return err
			}
			continue
		}

		if !fieldValue.CanSet() {
			continue
		}

		tagValue, hasTagValue := fieldType.Tag.Lookup(TagValue)
		if !hasTagValue {
			continue
		}

		name, options := parseTag(tagValue)
		isRequired := options.Contains("required")
		defValue, hasDefValue := fieldType.Tag.Lookup(TagDefault)

		env, found := os.LookupEnv(name)
		if isRequired && !found {
			return fmt.Errorf("environment variable %s is required but the value is not provided", name)
		}

		if !found && hasDefValue {
			env = defValue
		}

		err := parseValue(fieldValue, name, env)
		if err != nil {
			return err
		}
	}

	return nil
}

func parseValue(fieldValue reflect.Value, name, env string) error {
	fieldType := fieldValue.Type()

	if parser, ok := parsers[fieldType]; ok {
		if err := parser(&fieldValue, env); err != nil {
			return fmt.Errorf("can't parse environment variable %v, err: %w", name, err)
		}
		return nil
	}

	if fieldValue.CanInterface() {
		var err error
		var isUnmarshaled bool
		if u, ok := fieldValue.Interface().(encoding.TextUnmarshaler); ok {
			err = u.UnmarshalText([]byte(env))
			isUnmarshaled = true
		} else if up, ok := fieldValue.Addr().Interface().(encoding.TextUnmarshaler); ok {
			err = up.UnmarshalText([]byte(env))
			isUnmarshaled = true
		}

		if err != nil {
			return fmt.Errorf("can't parse environment variable %v, err: %w", name, err)
		}

		if isUnmarshaled {
			return nil
		}
	}

	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(env)

	case reflect.Bool:
		b, err := strconv.ParseBool(env)
		if err != nil {
			return fmt.Errorf("can't parse environment variable %v", name)
		}
		fieldValue.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		number, err := strconv.ParseInt(env, 0, fieldType.Bits())
		if err != nil {
			return fmt.Errorf("can't parse environment variable %v", name)
		}
		fieldValue.SetInt(number)

	case reflect.Int64:
		if fieldType == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(env)
			if err != nil {
				return err
			}
			fieldValue.SetInt(int64(d))
		} else {
			number, err := strconv.ParseInt(env, 0, fieldType.Bits())
			if err != nil {
				return err
			}
			fieldValue.SetInt(number)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, err := strconv.ParseUint(env, 0, fieldType.Bits())
		if err != nil {
			return err
		}
		fieldValue.SetUint(number)

	case reflect.Float32, reflect.Float64:
		number, err := strconv.ParseFloat(env, fieldType.Bits())
		if err != nil {
			return err
		}
		fieldValue.SetFloat(number)

	case reflect.Map:
		mapValue, err := parseMap(fieldType, name, env, ",")
		if err != nil {
			return err
		}

		fieldValue.Set(*mapValue)

	case reflect.Ptr:
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldType.Elem()))
		}

		return parseValue(fieldValue.Elem(), name, env)

	default:
		return fmt.Errorf("unsupported type %s", fieldValue.Kind())
	}

	return nil
}

func parseMap(valueType reflect.Type, name, env, sep string) (*reflect.Value, error) {
	mapValue := reflect.MakeMap(valueType)
	if len(strings.TrimSpace(env)) != 0 {
		pairs := strings.Split(env, sep)
		for _, pair := range pairs {
			elem := strings.SplitN(pair, ":", 2)
			if len(elem) != 2 {
				return nil, fmt.Errorf("can't parse environment variable %v, err: invalid map item %q", name, pair)
			}

			key := reflect.New(valueType.Key()).Elem()
			err := parseValue(key, name, elem[0])
			if err != nil {
				return nil, err
			}

			value := reflect.New(valueType.Elem()).Elem()
			err = parseValue(value, name, elem[1])
			if err != nil {
				return nil, err
			}

			mapValue.SetMapIndex(key, value)
		}
	}

	return &mapValue, nil
}

type tagOptions string

func parseTag(tag string) (string, tagOptions) {
	tag, opt, _ := strings.Cut(tag, ",")
	return tag, tagOptions(opt)
}

func (o tagOptions) Contains(optionName string) bool {
	if len(o) == 0 {
		return false
	}
	s := string(o)
	for s != "" {
		var name string
		name, s, _ = strings.Cut(s, ",")
		if name == optionName {
			return true
		}
	}
	return false
}
