package main

import (
	"errors"
	"math/big"
	"strings"
)

var (
	ErrUnsupported    = errors.New("unsupported input format")
	ErrDivisionByZero = errors.New("unsupported divide by zero")
)

type Calculator struct {
	Display  string
	Operands [2]*big.Float
	Operator *rune
}

func NewCalculator() *Calculator {
	return new(Calculator).reset()
}

func (c *Calculator) reset() *Calculator {
	c.Display = "0"
	c.Operator = nil
	c.Operands = [2]*big.Float{}
	return c
}

func (c *Calculator) ProcessOperand(r rune) error {
	isDigit := '0' <= r && r <= '9'
	if !(isDigit || r == '.' && !strings.ContainsRune(c.Display, '.')) {
		return ErrUnsupported
	}

	for _, s := range []string{"NaN", "Inf", "+Inf", "-Inf"} {
		if c.Display == s {
			c.reset()
			break
		}
	}

	if c.Operator != nil && c.Operands[1] == nil {
		c.Display = "0"
		c.Operands[1] = new(big.Float)
	}

	if c.Display == "0" && r != '.' {
		c.Display = string(r)
	} else {
		c.Display += string(r)
	}
	return nil
}

func (c *Calculator) ProcessOperator(r rune) error {
	switch r {
	case 'C':
		c.Display = "0"
		return nil
	case 'T':
		if c.Display == "0" {
			return nil
		}

		if strings.HasPrefix(c.Display, "-") {
			c.Display = c.Display[1:]
		} else {
			c.Display = "-" + c.Display
		}
		return nil
	case '=':
		return c.calculate()
	default:
		if !strings.ContainsRune("%/*-+", r) || c.Operator != nil {
			return ErrUnsupported
		}

		operand, ok := new(big.Float).SetString(c.Display)
		if !ok {
			return ErrUnsupported
		}

		c.Operands[0] = operand
		c.Operator = &r
		return nil
	}
}

func (c *Calculator) calculate() error {
	if c.Operands[1] == nil {
		return ErrUnsupported
	}

	operand, ok := new(big.Float).SetString(c.Display)
	if !ok {
		return ErrUnsupported
	}

	result := new(big.Float)
	c.Operands[1] = operand

	switch *c.Operator {
	case '+':
		result.Add(c.Operands[0], c.Operands[1])
	case '-':
		result.Sub(c.Operands[0], c.Operands[1])
	case '*':
		result.Mul(c.Operands[0], c.Operands[1])
	case '/':
		if c.Operands[1].Sign() == 0 {
			return ErrDivisionByZero
		}
		result.Quo(c.Operands[0], c.Operands[1])
	case '%':
		if c.Operands[1].Sign() == 0 {
			return ErrDivisionByZero
		}

		quo := new(big.Float).Quo(c.Operands[0], c.Operands[1])
		floorInt, _ := quo.Int(nil)
		floor := new(big.Float).SetInt(floorInt)
		mul := new(big.Float).Mul(c.Operands[1], floor)
		result = new(big.Float).Sub(c.Operands[0], mul)
	}

	if result.Sign() == 0 {
		result = new(big.Float).SetFloat64(0)
	}

	c.reset()
	c.Display = strings.TrimSuffix(result.Text('f', -1), ".0")
	return nil
}
