package tui

import (
	"bufio"
	"io"
	"os"
	"time"
)

// KeyType identifies the kind of terminal input event.
type KeyType int

const (
	KeyNone KeyType = iota
	KeyRune
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyEnter
	KeyEsc
	KeyTab
	KeyBackTab
	KeySpace
	KeyBackspace
	KeyDelete
	KeyCtrlC
	KeyCtrlD
	KeyCtrlQ
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
)

// Key represents a keyboard event.
type Key struct {
	Type KeyType
	Rune rune
}

// Matches returns true if the key represents either the given English or Russian rune.
func (k Key) Matches(en, ru rune) bool {
	if k.Type == KeySpace && (en == ' ' || ru == ' ') {
		return true
	}
	if k.Type != KeyRune {
		return false
	}
	r := k.Rune
	return r == en || r == ru ||
		(en >= 'a' && en <= 'z' && r == en-32) ||
		(ru != 0 && r == ru)
}

// KeyReader reads keys asynchronously from stdin and delivers them to a channel.
type KeyReader struct {
	ch   chan Key
	done chan struct{}
}

// NewKeyReader starts reading key events from stdin in a background goroutine.
func NewKeyReader() *KeyReader {
	kr := &KeyReader{
		ch:   make(chan Key, 32),
		done: make(chan struct{}),
	}
	go kr.readLoop()
	return kr
}

// C returns the channel receiving key events.
func (kr *KeyReader) C() <-chan Key {
	return kr.ch
}

// Close stops the key reader.
func (kr *KeyReader) Close() {
	close(kr.done)
}

func (kr *KeyReader) readLoop() {
	reader := bufio.NewReader(os.Stdin)

	for {
		select {
		case <-kr.done:
			return
		default:
		}

		r, _, err := reader.ReadRune()
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		switch r {
		case '\r', '\n':
			kr.send(Key{Type: KeyEnter})
		case '\t':
			kr.send(Key{Type: KeyTab})
		case ' ':
			kr.send(Key{Type: KeySpace, Rune: ' '})
		case 0x7f, 0x08:
			kr.send(Key{Type: KeyBackspace})
		case 0x03:
			kr.send(Key{Type: KeyCtrlC})
		case 0x04:
			kr.send(Key{Type: KeyCtrlD})
		case 0x11:
			kr.send(Key{Type: KeyCtrlQ})
		case 0x1b: // ESC or escape sequence
			// Check if another byte follows immediately (within 25ms)
			if reader.Buffered() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if reader.Buffered() == 0 {
				kr.send(Key{Type: KeyEsc})
				continue
			}

			next, _, err := reader.ReadRune()
			if err != nil {
				kr.send(Key{Type: KeyEsc})
				continue
			}

			if next == '[' {
				kr.parseCSI(reader)
			} else if next == 'O' {
				kr.parseSS3(reader)
			} else {
				kr.send(Key{Type: KeyEsc})
			}
		default:
			kr.send(Key{Type: KeyRune, Rune: r})
		}
	}
}

func (kr *KeyReader) send(k Key) {
	select {
	case kr.ch <- k:
	case <-kr.done:
	}
}

func (kr *KeyReader) parseCSI(r *bufio.Reader) {
	b, err := r.ReadByte()
	if err != nil {
		return
	}

	switch b {
	case '[':
		// Linux console F1..F5: ESC [ [ A .. E
		nb, err := r.ReadByte()
		if err != nil {
			return
		}
		switch nb {
		case 'A':
			kr.send(Key{Type: KeyF1})
		case 'B':
			kr.send(Key{Type: KeyF2})
		case 'C':
			kr.send(Key{Type: KeyF3})
		case 'D':
			kr.send(Key{Type: KeyF4})
		case 'E':
			kr.send(Key{Type: KeyF5})
		}
	case 'A':
		kr.send(Key{Type: KeyUp})
	case 'B':
		kr.send(Key{Type: KeyDown})
	case 'C':
		kr.send(Key{Type: KeyRight})
	case 'D':
		kr.send(Key{Type: KeyLeft})
	case 'H':
		kr.send(Key{Type: KeyHome})
	case 'F':
		kr.send(Key{Type: KeyEnd})
	case 'P':
		kr.send(Key{Type: KeyF1})
	case 'Q':
		kr.send(Key{Type: KeyF2})
	case 'R':
		kr.send(Key{Type: KeyF3})
	case 'S':
		kr.send(Key{Type: KeyF4})
	case 'Z':
		kr.send(Key{Type: KeyBackTab})
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		// Extended sequence like ESC [ 3 ~ or ESC [ 1 ; 2 A
		var params []byte
		params = append(params, b)
		for {
			nb, err := r.ReadByte()
			if err != nil {
				break
			}
			if nb == '~' {
				switch string(params) {
				case "1", "7":
					kr.send(Key{Type: KeyHome})
				case "4", "8":
					kr.send(Key{Type: KeyEnd})
				case "2":
					// Insert
				case "3":
					kr.send(Key{Type: KeyDelete})
				case "5":
					kr.send(Key{Type: KeyPgUp})
				case "6":
					kr.send(Key{Type: KeyPgDn})
				case "11":
					kr.send(Key{Type: KeyF1})
				case "12":
					kr.send(Key{Type: KeyF2})
				case "13":
					kr.send(Key{Type: KeyF3})
				case "14":
					kr.send(Key{Type: KeyF4})
				case "15":
					kr.send(Key{Type: KeyF5})
				}
				return
			}
			if (nb >= 'A' && nb <= 'Z') || (nb >= 'a' && nb <= 'z') {
				switch nb {
				case 'A':
					kr.send(Key{Type: KeyUp})
				case 'B':
					kr.send(Key{Type: KeyDown})
				case 'C':
					kr.send(Key{Type: KeyRight})
				case 'D':
					kr.send(Key{Type: KeyLeft})
				case 'P':
					kr.send(Key{Type: KeyF1})
				case 'Q':
					kr.send(Key{Type: KeyF2})
				case 'R':
					kr.send(Key{Type: KeyF3})
				case 'S':
					kr.send(Key{Type: KeyF4})
				case 'Z':
					kr.send(Key{Type: KeyBackTab})
				}
				return
			}
			params = append(params, nb)
			if len(params) > 24 {
				return
			}
		}
	}
}

func (kr *KeyReader) parseSS3(r *bufio.Reader) {
	b, err := r.ReadByte()
	if err != nil {
		return
	}
	switch b {
	case 'A':
		kr.send(Key{Type: KeyUp})
	case 'B':
		kr.send(Key{Type: KeyDown})
	case 'C':
		kr.send(Key{Type: KeyRight})
	case 'D':
		kr.send(Key{Type: KeyLeft})
	case 'H':
		kr.send(Key{Type: KeyHome})
	case 'F':
		kr.send(Key{Type: KeyEnd})
	case 'P':
		kr.send(Key{Type: KeyF1})
	case 'Q':
		kr.send(Key{Type: KeyF2})
	case 'R':
		kr.send(Key{Type: KeyF3})
	case 'S':
		kr.send(Key{Type: KeyF4})
	}
}
