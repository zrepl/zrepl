package main

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestSampleConfigFileIsParsedWithoutErrors(t *testing.T) {
	_, err := ParseConfig("./sampleconf/zrepl.yml")
	assert.Nil(t, err)
}
