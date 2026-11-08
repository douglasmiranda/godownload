package urlgen

import (
	"bufio"
	"io"
	"os"
	"strings"
)

func LocalFileLoader(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadLines(f)
}

// ReadLines splits r into lines, dropping a leading UTF-8 byte order mark and
// trailing carriage returns. Lines may be of any length.
func ReadLines(r io.Reader) ([]string, error) {
	br := bufio.NewReader(r)
	out := []string{}
	for first := true; ; first = false {
		line, err := br.ReadString('\n')
		if line != "" || err == nil {
			line = strings.TrimRight(line, "\r\n")
			if first {
				line = strings.TrimPrefix(line, "\ufeff")
			}
			out = append(out, line)
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
