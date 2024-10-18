// Package nodefault provides newtypes around builtin Go types
// so that if the newtype is used as a field in a struct and that field
// is zero-initialized (https://golang.org/ref/spec#The_zero_value),
// accessing the field will cause a null pointer deref panic.
// Or in other terms: It soft-enforces that the caller sets the field
// explicitly when constructing the struct.
//
// Example:
//
//	type Config struct {
//	    // This field must be set to a non-nil value,
//	    // forcing the caller to make their mind up
//	    // about this field.
//	    CriticalSetting   *nodefault.Bool
//	}
//
// An function that takes such a Config should _not_ check for nil-ness:
// and instead unconditionally dereference:
//
//	func f(c Config) {
//	    if (c.CriticalSetting) { }
//	}
//
// If the caller of f forgot to specify the .CriticalSetting
// field, the Go runtime will issue a nil-pointer deref panic
// and it'll be clear that the caller did not read the docs of Config.
//
//	f(Config{}) // crashes
//
//	f Config{ CriticalSetting: &nodefault.Bool{B: false}} // doesn't crash
package nodefault
