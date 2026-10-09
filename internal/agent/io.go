package agent

import "io"

func bufCopy(dst io.StringWriter, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var n int64
	for n < limit {
		want := int64(len(buf))
		if limit-n < want {
			want = limit - n
		}
		m, err := src.Read(buf[:want])
		if m > 0 {
			_, _ = dst.WriteString(string(buf[:m]))
			n += int64(m)
		}
		if err != nil {
			return n, nil
		}
	}
	return n, nil
}
