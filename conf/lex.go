// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pos represents a byte position in the original input text from which
// this template was parsed.
type Pos int

// item represents a token or text string returned from the scanner.
type item struct {
	typ itemType // The type of this item.
	pos Pos      // The starting position, in bytes, of this item in the input string.
	val string   // The value of this item.
}

func (i item) String() string {
	switch {
	case i.typ == itemEOF:
		return "EOF"
	case i.typ == itemError:
		return i.val
	case len(i.val) > 10:
		return fmt.Sprintf("%.10q...", i.val)
	}
	return fmt.Sprintf("%q", i.val)
}

// itemType identifies the type of lex items.
type itemType int

const (
	itemError itemType = iota // error occurred; value is text of error
	itemEOF
	itemEqual      // '='
	itemLeftDelim  // '['
	itemRawString  // raw string (includes quotes)
	itemIdentifier // identifier for section and value names
	itemRightDelim // ']'
	itemString     // string (no quotes)
)

const eof = -1

// stateFn represents the state of the scanner as a function that returns the next state.
type stateFn func(*lexer) stateFn

// lexer holds the state of the scanner.
type lexer struct {
	name    string    // the name of the input; used only for error reports
	input   string    // the string being scanned
	state   stateFn   // the next lexing function to enter
	pos     Pos       // current position in the input
	start   Pos       // start position of this item
	width   Pos       // width of last rune read from input
	lastPos Pos       // position of most recent item returned by nextItem
	items   chan item // channel of scanned items
}

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if int(l.pos) >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = Pos(w)
	l.pos += l.width
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// backup steps back one rune. Can only be called once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.items <- item{t, l.start, l.input[l.start:l.pos]}
	l.start = l.pos
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid string) bool {
	if strings.IndexRune(valid, l.next()) >= 0 {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes a run of runes from the valid set.
func (l *lexer) acceptRun(valid string) {
	for strings.IndexRune(valid, l.next()) >= 0 {
	}
	l.backup()
}

// lineNumber reports which line we're on, based on the position of
// the previous item returned by nextItem. Doing it this way
// means we don't have to worry about peek double counting.
func (l *lexer) lineNumber() int {
	return 1 + strings.Count(l.input[:l.lastPos], "\n")
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating l.nextItem.
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.items <- item{itemError, l.start, fmt.Sprintf(format, args...)}
	return nil
}

// nextItem returns the next item from the input.
func (l *lexer) nextItem() item {
	item := <-l.items
	l.lastPos = item.pos
	return item
}

// lex creates a new scanner for the input string.
func lex(name, input string) *lexer {
	l := &lexer{
		name:  name,
		input: input,
		items: make(chan item),
	}
	go l.run()
	return l
}

// run runs the state machine for the lexer.
func (l *lexer) run() {
	for l.state = lexSpace; l.state != nil; {
		l.state = l.state(l)
	}
}

// state functions

const (
	leftDelim  = '['
	rightDelim = ']'
)

// lexSpace scans until start of section or value
func lexSpace(l *lexer) stateFn {
Loop:
	for {
		switch r := l.next(); {
		case r == leftDelim:
			return lexLeftDelim
		case isVarchar(r):
			l.backup()
			return lexValue
		case isSpace(r) || isEndOfLine(r):
			// ignore
		case r == eof:
			l.emit(itemEOF)
			break Loop
		default:
			return l.errorf("invalid character")
		}
	}
	return nil
}

func lexLeftDelim(l *lexer) stateFn {
	l.emit(itemLeftDelim)
	return lexVarname
}

func lexVarname(l *lexer) stateFn {
	l.ignore()
	for {
		switch r := l.next(); {
		case isVarchar(r):
			// absorb
		case r == rightDelim:
			l.backup()
			l.emit(itemIdentifier)
			return lexRightDelim
		default:
			return l.errorf("invalid varname")
		}
	}
}

func lexRightDelim(l *lexer) stateFn {
	l.pos += Pos(len(string(rightDelim)))
	l.emit(itemRightDelim)
	return lexSpace
}

func lexValue(l *lexer) stateFn {
	l.ignore()
	for {
		switch r := l.next(); {
		case isVarchar(r):
			// absorb
		default:
			l.backup()
			l.emit(itemIdentifier)
			return lexEqual
		}
	}
}

func lexEqual(l *lexer) stateFn {
Loop:
	for {
		switch r := l.next(); {
		case r == '=':
			l.emit(itemEqual)
			break Loop
		case isSpace(r):
			l.ignore()
		default:
			return l.errorf("expected =")
		}
	}
	for isSpace(l.peek()) {
		l.next()
	}
	l.ignore()
	if l.peek() == '`' {
		return lexRawString
	}
	return lexString
}

func lexString(l *lexer) stateFn {
	for {
		switch r := l.next(); {
		case isEndOfLine(r) || r == eof:
			l.emit(itemString)
			return lexSpace
		}
	}
}

func lexRawString(l *lexer) stateFn {
	l.next()
Loop:
	for {
		switch l.next() {
		case eof:
			return l.errorf("unterminated raw string")
		case '`':
			break Loop
		}
	}
	l.emit(itemRawString)
	return lexSpace
}

// isSpace reports whether r is a space character.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

// isEndOfLine reports whether r is an end-of-line character.
func isEndOfLine(r rune) bool {
	return r == '\r' || r == '\n'
}

func isVarchar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}