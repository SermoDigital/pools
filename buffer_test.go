package pools

import (
	"bytes"
	"strconv"
	"testing"
)

func expect(t *testing.T, want, got interface{}) {
	if want != got {
		t.Fatalf("want %v, got %v\n", want, got)
	}
}

func TestBufferSequentialGroup(t *testing.T) {
	w := GetBuffer()
	w.WriteGroups(1, 1, 5)
	expect(t, " ($1), ($2), ($3), ($4), ($5)", w.String())
}

func TestBufferSequentialInterval(t *testing.T) {
	w := GetBuffer()
	w.WriteInterval(1, 5, 1)
	expect(t, " ($1, $2, $3, $4, $5)", w.String())
}

func TestBuffer_WriteInterval(t *testing.T) {
	w := GetBuffer()
	w.WriteInterval(0, 4, 2)
	expect(t, " ($0, $1, $2, $3, $4), ($0, $1, $2, $3, $4)", w.String())
}

func TestBuffer_WriteGroups(t *testing.T) {
	w := GetBuffer()
	w.WriteGroups(0, 4, 2)
	expect(t, " ($0, $1, $2, $3), ($4, $5, $6, $7)", w.String())
}

func TestBuffer_WriteGroupsPrefix(t *testing.T) {
	w := GetBuffer()
	w.WriteGroups(2, 1, 2, 1)
	expect(t, " ($1, $2), ($1, $3)", w.String())
}

var bbb []byte

func BenchmarkBuffer_WriteInt(b *testing.B) {
	var buf Buffer
	for i := 0; i < b.N; i++ {
		buf.WriteInt(i)
	}
	bbb = buf.Bytes()
}

type testBuffer struct{ bytes.Buffer }

func BenchmarkTestBuffer_WriteInt(b *testing.B) {
	var tb testBuffer
	for i := 0; i < b.N; i++ {
		tb.WriteString(strconv.Itoa(i))
	}
	bbb = tb.Bytes()
}
