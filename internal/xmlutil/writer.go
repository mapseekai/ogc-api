// Package xmlutil centralizes bounded, escaped XML output.
package xmlutil

import (
	"bytes"
	"encoding/xml"
	"errors"
)

type Writer struct {
	buf   limitedBuffer
	enc   *xml.Encoder
	stack []xml.StartElement
	err   error
}

func New(max int) *Writer {
	w := &Writer{buf: limitedBuffer{max: max}}
	w.enc = xml.NewEncoder(&w.buf)
	w.enc.Indent("", "  ")
	w.token(xml.ProcInst{Target: "xml", Inst: []byte(`version="1.0" encoding="UTF-8"`)})
	return w
}

func (w *Writer) Optional(name, value string) {
	if value != "" {
		w.Element(name, value)
	}
}

func (w *Writer) Element(name, value string, attrs ...Attr) {
	w.Start(name, attrs...)
	w.Text(value)
	w.End()
}

func (w *Writer) Start(name string, attrs ...Attr) {
	el := xml.StartElement{Name: xml.Name{Local: name}, Attr: attrs}
	w.stack = append(w.stack, el)
	w.token(el)
}

func (w *Writer) End() {
	if len(w.stack) == 0 {
		w.err = errors.New("unbalanced XML writer")
		return
	}
	i := len(w.stack) - 1
	w.token(w.stack[i].End())
	w.stack = w.stack[:i]
}

func (w *Writer) Text(value string) { w.token(xml.CharData(value)) }

func (w *Writer) Directive(value string) { w.token(xml.Directive(value)) }

func (w *Writer) Bytes() ([]byte, error) {
	if len(w.stack) != 0 {
		return nil, errors.New("unclosed XML elements")
	}
	if w.err != nil {
		return nil, w.err
	}
	if err := w.enc.Flush(); err != nil {
		return nil, err
	}
	return w.buf.Bytes(), nil
}

func (w *Writer) token(t xml.Token) {
	if w.err == nil {
		w.err = w.enc.EncodeToken(t)
	}
}

type Attr = xml.Attr

func A(name, value string) Attr { return Attr{Name: xml.Name{Local: name}, Value: value} }

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, errors.New("XML exceeds the configured limit")
	}
	return b.Buffer.Write(p)
}
