package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSampleConfigFileIsParsedWithoutErrors(t *testing.T) {
	_, err := ParseConfig("./sampleconf/zrepl.yml")
	assert.Nil(t, err)
}
