package pools

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/sermodigital/errors"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(Buffer)
	},
}

func GetBuffer() *Buffer {
	return bufferPool.Get().(*Buffer)
}

// UnsafeBytes returns a slice of bytes that will automatically add the Buffer
// it came from back into the pool when the GC attempts to collect it. This is
// only useful when the returned byte slice needs to 'outlive' the buffer,
// but an allocation isn't preferable. For example:
//
//	func Marshal(v interface{}) ([]byte, error) {
//		b := pools.GetBuffer()
//		defer pools.PutBuffer()
//
//		err := json.NewEncoder(b).Encode()
//		return b.Bytes(), err
//	}
//
//	func MyHandler(w http.ResponseWriter, r *http.Request) {
//		b, err := Marshal("hello, world!")
//		if err != nil {
//			http.Error(w, err.String(), http.StatusInternalServerError)
//			return
//		}
//
//		// The Buffer created in the 'Marshal' call has already been put back
// 		// into the pool. Any other goroutine can now collect it from the pool.
//		// New writes to the Buffer will overwrite 'b' since 'b' points directly
//		// to the Buffer's internal buffer. This can corrupt data and race
//		// conditions in Go are undefined behavior.
//		_, err := w.Write(b)
//		if err != nil {
//			...
//		}
//	}
//
// By calling UnsafeBytes instead, the data would be protected.
//
// IMPORTANT: Do not call pools.PutBuffer on the Buffer if you call UnsafeBytes
// at all. Additionally, do not append to the returned slice. Reallocation will
// invalidate the Buffer.
func (b *Buffer) UnsafeBytes() []byte {
	// Fast path–the Buffer is empty so return nil and place the Buffer back
	// into the pool. This could still panic if:
	// 	- UnsafeBytes is called
	// 	- b is drained
	//	- UnsafeBytes is called again
	if b.Len() == 0 {
		PutBuffer(b)
		return nil
	}

	// Prevent multiple calls to UnsafeBytes. We do not want multiple finalizers
	// set.
	if !atomic.CompareAndSwapUint32(&b.unsafe, 0, 1) {
		panic("pools: UnsafeBytes called twice")
	}

	buf := b.Bytes()
	runtime.SetFinalizer(&buf[0], func(c *byte) {
		// If, somehow, b.unsafe != 1 panic. This means I goofed up and missed
		// something somewhere.
		if !atomic.CompareAndSwapUint32(&b.unsafe, 1, 0) {
			panic("pools: Buffer.unsafe is not 1")
		}
		PutBuffer(b)
	})
	return buf
}

func PutBuffer(b *Buffer) {
	// If everything else holds true b.unsafe will be zero. Anything else is
	// invalid.
	if atomic.LoadUint32(&b.unsafe) != 0 {
		panic("pools: PutBuffer called after UnsafeBytes without finalizer running")
	}
	b.Reset()
	bufferPool.Put(b)
}

type Buffer struct {
	unsafe uint32 // 1 if UnsafeBytes was called.
	bytes.Buffer
}

// WriteInt64 is a wrapper that writes i to w.
func (w *Buffer) WriteInt64(i int64) {
	w.WriteString(strconv.FormatInt(i, 10))
}

// WriteInt is a wrapper that writes i to w.
func (w *Buffer) WriteInt(i int) {
	w.WriteString(strconv.Itoa(i))
}

func (w *Buffer) grow(start, end, num int) {
	width := totalWidth(end-start+1, 3) // 3: '$, '
	// +2: "()"
	// -2: last interval doesn't have a trailing ', '
	x := int((width+2)*num - 2)

	const intSize = (32 << (^uint(0) >> 63)) - 1

	// x > 0 ? x : 0.
	w.Grow(x & ^(x >> intSize))
}

// WriteGroups writes the interval [offset, offset+groupLen) to w N times.
// Each number is prefixed with '$' and suffixed with ', '. The final value in
// an interval and final interval in a set are not suffixed with ', '. The
// intervals are wrapped in parenthases. An error is only returned if the
// arguments are invalid. Arguments are invalid if offset < 0 or groups == 0.
//
// 	WriteInterval(0, 4, 2) // ($0, $1, $2, $3, $4), ($5, $6, $7, $8, $9)
//
func (w *Buffer) WriteGroups(offset, groupLen, groups int, prefix ...int) error {
	if offset < 0 || groups == 0 {
		return errors.New("invalid arguments to WriteGroups")
	}
	w.grow(0, groupLen, groups)
	offset += w.writeGroup(prefix, offset, groupLen)

	// Assuming we have more to write...
	for groups--; groups > 0; groups-- {
		w.WriteByte(',')
		offset += w.writeGroup(prefix, offset, groupLen)
	}
	return nil
}

func (w *Buffer) writeGroup(prefix []int, offset, groupLen int) int {
	w.WriteString(" ($")
	for _, v := range prefix {
		w.WriteInt(v)
		w.WriteString(", $")
	}
	w.WriteInt(offset)
	for i := 1; i < groupLen; i, offset = i+1, offset+1 {
		w.WriteString(", $")
		w.WriteInt(offset + 1)
	}
	w.WriteByte(')')
	return groupLen
}

// WriteInterval writes the interval [start, end] to w N times. Each number is
// prefixed with '$' and suffixed with ', '. The final value in an interval
// and final interval in a set are not suffixed with ', '. The intervals are
// wrapped in parenthases. An error is only returned if the arguments are
// invalid. Arguments are invalid if start < 0, start >= end, or num == 0.
//
// 	WriteInterval(0, 4, 2) // (0, 1, 2, 3, 4), (0, 1, 2, 3, 4)
//
func (w *Buffer) WriteInterval(start, end, num int) error {
	if start < 0 || start >= end || num == 0 {
		return errors.New("invalid arguments to WriteInterval")
	}

	w.grow(start, end, num)

	w.WriteString(" ($")
	w.WriteInt(start)
	for i := start; i < end; i++ {
		w.WriteString(", $")
		w.WriteInt(i + 1)
	}
	w.WriteByte(')')

	// Assuming we have more to write...
	for num--; num > 0; num-- {
		w.WriteString(", ($")
		w.WriteInt(start)
		for i := start; i < end; i++ {
			w.WriteString(", $")
			w.WriteInt(i + 1)
		}
		w.WriteByte(')')
	}

	return nil
}

// {pow, sum}
var cache = [...][2]int{
	{0, 0},
	{9, 9},
	{99, 189},
	{999, 2889},
	{9999, 38889},
	{99999, 488889},
	{999999, 5888889},
	{9999999, 68888889},
	{99999999, 788888889},
	{999999999, 8888888889},
	{9999999999, 98888888889},
	{99999999999, 1088888888889},
	{999999999999, 11888888888889},
	{9999999999999, 128888888888889},
	{99999999999999, 1388888888888889},
	{999999999999999, 14888888888888889},
	{9999999999999999, 158888888888888889},
	{99999999999999999, 1688888888888888889},
}

// totalWidth finds the cumulative length of all numbers in the range [1, n];
// add is added to each number. If n <= 0 add is returned.
func totalWidth(n, add int) int {
	switch {
	case n <= 0:
		return add
	case n < 10:
		return n + n*add
	case n < 100:
		return n*2 - 9 + n*add
	case n > cache[len(cache)-1][0]:
		return 0
	}
	for i := 3; ; i++ {
		if n <= cache[i][0] {
			return (n-cache[i-1][0])*i + cache[i-1][1] + n*add
		}
	}
}
