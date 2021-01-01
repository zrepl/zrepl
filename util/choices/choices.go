// Package choice implements a flag.Value type that accepts a set of choices.
//
// See test cases or grep the code base for usage hints.
package choices

import (
	"flag"
	"fmt"
	"strings"
)

type Choices struct {
	choices    map[string]interface{}
	typeString string
	value      interface{}
}

var _ flag.Value = (*Choices)(nil)

func new(pairs ...interface{}) Choices {
	if (len(pairs) % 2) != 0 {
		panic("must provide a sequence of key value pairs")
	}
	c := Choices{
		choices: make(map[string]interface{}, len(pairs)/2),
		value:   nil,
	}
	for i := 0; i < len(pairs); {
		key, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("argument %d is %T but should be a string, value: %#v", i, pairs[i], pairs[i]))
		}
		c.choices[key] = pairs[i+1]
		i += 2
	}
	c.typeString = strings.Join(c.choicesList(true), ",") // overrideable by setter
	return c
}

func (c *Choices) Init(pairs ...interface{}) {
	*c = new(pairs...)
}

func (c Choices) choicesList(escaped bool) []string {
	keys := make([]string, len(c.choices))
	i := 0
	for k := range c.choices {
		e := k
		if escaped {
			e = fmt.Sprintf("%q", k)
		}
		keys[i] = e
		i += 1
	}
	return keys
}

func (c Choices) Usage() string {
	return fmt.Sprintf("one of %s", strings.Join(c.choicesList(true), ","))
}

func (c *Choices) SetDefaultValue(v interface{}) {
	c.value = v
}

func (c Choices) Value() interface{} {
	return c.value
}

func (c *Choices) Set(input string) error {
	v, ok := c.choices[input]
	if !ok {
		return fmt.Errorf("invalid value %q: must be one of %s", input, c.Usage())
	}
	c.value = v
	return nil
}

func (c *Choices) String() string {
	return "" // c.value.(fmt.Stringer).String()
}

func (c *Choices) SetTypeString(ts string) {
	c.typeString = ts
}

func (c *Choices) Type() string {
	return c.typeString
}
