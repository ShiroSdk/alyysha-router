// Alyysha Router — this file is part of a port maintained by https://github.com/ShiroSdk/ — https://t.me/YannClippers
package guardrails

// CustomPattern defines a user-specified regex pattern to match and sanitize.
type CustomPattern struct {
	Name        string `json:"name"`
	Regex       string `json:"regex"`
	Replacement string `json:"replacement,omitempty"`
}
