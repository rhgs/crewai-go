// Package tools groups ready-to-use tools for agents.
package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/rhgs/crewai-go"
)

// Calculator returns a tool that evaluates arithmetic expressions with
// +, -, *, /, parentheses, and decimal numbers. It is fully offline and safe
// (no third-party eval).
func Calculator() crewai.Tool {
	return crewai.NewTool(
		"calculator",
		"Evaluates an arithmetic expression (e.g. '2 + 2 * (3 - 1)'). "+
			"The input must be the expression only.",
		func(_ context.Context, input string) (string, error) {
			val, err := evalExpr(strings.TrimSpace(input))
			if err != nil {
				return "", err
			}
			return strconv.FormatFloat(val, 'g', -1, 64), nil
		},
	)
}

// evalExpr evaluates a simple arithmetic expression.
func evalExpr(s string) (float64, error) {
	p := &parser{src: s}
	v, err := p.parseExpression()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos != len(p.src) {
		return 0, fmt.Errorf("invalid expression near %q", p.src[p.pos:])
	}
	return v, nil
}

// parser is a recursive-descent parser for arithmetic expressions.
//
//	expression = term { ("+" | "-") term }
//	term       = factor { ("*" | "/") factor }
//	factor     = number | "(" expression ")" | ("+"|"-") factor
type parser struct {
	src string
	pos int
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) parseExpression() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.src) {
			return v, nil
		}
		op := p.src[p.pos]
		if op != '+' && op != '-' {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += rhs
		} else {
			v -= rhs
		}
	}
}

func (p *parser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.src) {
			return v, nil
		}
		op := p.src[p.pos]
		if op != '*' && op != '/' {
			return v, nil
		}
		p.pos++
		rhs, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			v *= rhs
		} else {
			if rhs == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			v /= rhs
		}
	}
}

func (p *parser) parseFactor() (float64, error) {
	p.skipSpaces()
	if p.pos >= len(p.src) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	switch p.src[p.pos] {
	case '+':
		p.pos++
		return p.parseFactor()
	case '-':
		p.pos++
		v, err := p.parseFactor()
		return -v, err
	case '(':
		p.pos++
		v, err := p.parseExpression()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, fmt.Errorf("expected ')'")
		}
		p.pos++
		return v, nil
	default:
		return p.parseNumber()
	}
}

func (p *parser) parseNumber() (float64, error) {
	p.skipSpaces()
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if (c >= '0' && c <= '9') || c == '.' {
			p.pos++
			continue
		}
		break
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected number near %q", p.src[p.pos:])
	}
	return strconv.ParseFloat(p.src[start:p.pos], 64)
}
