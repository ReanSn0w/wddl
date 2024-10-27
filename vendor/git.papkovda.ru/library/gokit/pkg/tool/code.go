package tool

import (
	"crypto/sha256"
	"time"
)

func NewCodeGenerator(a, n bool, ttl, len int) *CodeGenerator {
	return &CodeGenerator{
		generator: NewRandom(a, n, false),
		ttl:       ttl,
		len:       len,
	}
}

type CodeGenerator struct {
	generator *Random
	ttl, len  int
}

func (c *CodeGenerator) Generate(seed string) string {
	codes := c.generateCodes(seed, c.ttl)
	return codes[c.ttl-1]
}

func (c *CodeGenerator) Check(seed, code string) bool {
	codes := c.generateCodes(seed, c.ttl)

	for _, value := range codes {
		if value == code {
			return true
		}
	}

	return false
}

func (c *CodeGenerator) generateCodes(input string, limit int) []string {
	codes := make([]string, 0)
	seed := c.seedConverter(input)
	currentTime := time.Now()

	for i := 0; i < limit; i++ {
		codeTimeUnix := time.Date(
			currentTime.Year(),
			currentTime.Month(),
			currentTime.Day(),
			currentTime.Hour(),
			currentTime.Minute()+i,
			0, 0,
			currentTime.Location()).Unix()

		codes = append(codes, c.generator.Pseudo(codeTimeUnix+seed, c.len))
	}

	return codes
}

func (c *CodeGenerator) seedConverter(input string) (result int64) {
	h := sha256.New()
	h.Write([]byte(input))
	hash := h.Sum(nil)

	for _, b := range hash {
		result += int64(b)
	}

	return
}
