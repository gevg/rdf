package parse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/knakk/rdf"
)

// Decoder implements a Turtle/Trig parser
type Decoder struct {
	r      *bufio.Reader
	l      *lexer
	format string

	cur, prev token
	err       error
}

// NewNTDecoder creates a N-Triples decoder
func NewNTDecoder(r io.Reader) *Decoder {
	return &Decoder{
		l:      newLexer(),
		r:      bufio.NewReader(r),
		format: "N-Triples",
	}
}

// Parser functions:

// Decode returns the next valid triple, or an error
func (d *Decoder) Decode() (rdf.Triple, error) {
	line, err := d.r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		d.l.stop() // reader drained, stop lexer
		return rdf.Triple{}, err
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 || bytes.HasPrefix(line, []byte("#")) {
		return d.Decode()
	}
	return d.parseNT(line)
}

func (d *Decoder) parseNT(line []byte) (rdf.Triple, error) {
	d.l.incoming <- line
	t := rdf.Triple{}

	// parse triple subject
	d.next()
	d.expect(tokenIRIAbs, tokenBNode)
	if d.err != nil {
		return t, d.err
	}
	if d.cur.typ == tokenIRIAbs {
		t.Subj = rdf.URI{URI: d.cur.text}
	} else {
		t.Subj = rdf.Blank{ID: d.cur.text}
	}

	// parse triple predicate
	d.next()
	d.expect(tokenIRIAbs, tokenBNode)
	if d.err != nil {
		return t, d.err
	}
	if d.cur.typ == tokenIRIAbs {
		t.Pred = rdf.URI{URI: d.cur.text}
	} else {
		t.Pred = rdf.Blank{ID: d.cur.text}
	}

	// parse triple object
	d.next()
	d.expect(tokenIRIAbs, tokenBNode, tokenLiteral)
	if d.err != nil {
		return t, d.err
	}

	switch d.cur.typ {
	case tokenBNode:
		t.Obj = rdf.Blank{ID: d.cur.text}
		d.next()
	case tokenLiteral:
		lit, err := d.parseLiteral(false)
		if err != nil {
			return t, err
		}
		t.Obj = lit
		if d.cur.typ == tokenDot {
			return t, nil
		}
		d.next()
	case tokenIRIAbs:
		t.Obj = rdf.URI{URI: d.cur.text}
		d.next()
	}

	// parse final dot (d.next() called in ojbect parse to check for langtag, datatype)
	d.expect(tokenDot)
	if d.err != nil {
		return t, d.err
	}

	// check for extra tokens, assert we reached end of line
	d.next()
	if d.cur.typ != tokenEOL {
		return t, fmt.Errorf("found extra token after end of statement: %q", d.cur.text)
	}

	return t, nil
}

// parseLiteral
func (d *Decoder) parseLiteral(relIRI bool) (rdf.Literal, error) {
	if d.cur.typ != tokenLiteral {
		panic("interal parse error: parseLiteral() expects current token to be a tokenLiteral")
	}
	l := rdf.Literal{}
	l.Val = d.cur.text
	l.DataType = rdf.XSDString
	if d.next() {
		switch d.cur.typ {
		case tokenLang:
			l.Lang = d.cur.text
			return l, nil
		case tokenDataTypeRel:
			if !relIRI {
				return l, errors.New("Literal data type IRI must be absolute")
			}
			fallthrough
		case tokenDataTypeAbs:
			l.DataType = rdf.URI{URI: d.cur.text}
			switch d.cur.text {
			case rdf.XSDInteger.URI:
				i, err := strconv.Atoi(d.prev.text)
				if err != nil {
					//TODO set datatype to xsd:string?
					return l, nil
				}
				l.Val = i
			case rdf.XSDFloat.URI: // TODO also XSDDouble ?
				f, err := strconv.ParseFloat(d.prev.text, 64)
				if err != nil {
					return l, nil
				}
				l.Val = f
			case rdf.XSDBoolean.URI:
				bo, err := strconv.ParseBool(d.prev.text)
				if err != nil {
					return l, nil
				}
				l.Val = bo
			case rdf.XSDDateTime.URI:
				t, err := time.Parse(rdf.DateFormat, d.prev.text)
				if err != nil {
					return l, nil
				}
				l.Val = t
				// TODO: other xsd dataypes
			}
			return l, nil
		default:
			// literal not follwed by language tag or datatype
			if d.cur.typ != tokenDot {
				d.backup()
			}
		}
	}
	return l, nil
}

func (d *Decoder) next() bool {
	if d.cur.typ != tokenEOL {
		d.prev = d.cur
	}
	d.cur = d.l.nextToken()
	if d.cur.typ == tokenEOL || d.cur.typ == tokenError {
		return false
	}
	return true
}

func (d *Decoder) backup() {
	d.cur = d.prev
}

func (d *Decoder) expect(tt ...tokenType) {
	if d.cur.typ == tokenEOL {
		d.err = fmt.Errorf("%d:%d: unexpected end of line", d.cur.line, d.cur.col)
		return
	}
	if d.cur.typ == tokenError {
		d.err = fmt.Errorf("%d:%d: syntax error: %s", d.cur.line, d.cur.col, d.cur.text)
		return
	}
	for i := range tt {
		if d.cur.typ == tt[i] {
			return
		}
	}
	if len(tt) == 1 {
		d.err = fmt.Errorf("%d:%d: expected %s, got %s", d.cur.line, d.cur.col, tt[0], d.cur.typ)
		return
	}
	var types = make([]string, 0, len(tt))
	for _, t := range tt {
		types = append(types, fmt.Sprintf("%s", t))
	}
	d.err = fmt.Errorf("%d:%d: expected %s, got %s", d.cur.line, d.cur.col, strings.Join(types, " / "), tt[0])
}

func (d *Decoder) oneOf(tt ...tokenType) bool {
	for i := range tt {
		if d.cur.typ == tt[i] {
			return true
		}
	}
	return false
}
