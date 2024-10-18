package platformtest

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitQuotedWords(t *testing.T) {
	s := bufio.NewScanner(strings.NewReader(`
	foo "bar baz" blah "foo \"with single escape" "blah baz" "\"foo" "foo\""
	`))
	s.Split(splitQuotedWords)
	var words []string
	for s.Scan() {
		words = append(words, s.Text())
	}
	assert.Equal(
		t,
		[]string{
			"foo",
			"bar baz",
			"blah",
			"foo \"with single escape",
			"blah baz",
			"\"foo",
			"foo\"",
		},
		words)

}
