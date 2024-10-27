package tool

import (
	"math/rand"
	"time"
)

const (
	alfabetValues = "QWERTYUIOPASDFGHJKLZXCVBNM"
	numbersValues = "1234567890"
	symbolsValues = "!@#$%^&*<>?"
)

func NewRandom(alphabet, numbers, symbols bool) *Random {
	chars := ""

	if alphabet {
		chars += alfabetValues
	}

	if numbers {
		chars += numbersValues
	}

	if symbols {
		chars += symbolsValues
	}

	return &Random{
		charset: chars,
	}
}

type Random struct {
	charset string
}

func (r *Random) Generate(count int) string {
	return r.Pseudo(time.Now().Unix(), count)
}

func (r *Random) Pseudo(seed int64, count int) string {
	random := rand.New(rand.NewSource(seed))

	randomLimit := len(r.charset)
	values := make([]byte, count)
	index := 0

	for i := 0; i < count; i++ {
		index = random.Intn(randomLimit)
		values[i] = r.charset[index]
	}

	return string(values)
}
