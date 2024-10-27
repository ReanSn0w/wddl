package tool

import (
	"fmt"
	"sort"
	"sync"
)

func NewAtomicSlice[T any]() *AtomicSlice[T] {
	return &AtomicSlice[T]{
		slice: make([]T, 0),
	}
}

type AtomicSlice[T any] struct {
	mx    sync.RWMutex
	slice []T
}

// Push - Добавляет элемент в конец среза
//
// Параметры для аргумента pos
// -1                       - добавит элемент в конец среза
// 0...n                    - добавит элемент на конкретную позици, если возможно
// n < -1 || n > len(slice) - произведет доавление в конец среза
func (a *AtomicSlice[T]) Push(pos int, value T) {
	a.mx.Lock()
	defer a.mx.Unlock()

	sliceLen := len(a.slice)

	if pos < -1 || pos > sliceLen {
		pos = -1
	}

	switch pos {
	case -1:
		a.slice = append(a.slice, value)
	case 0:
		a.slice = append([]T{value}, a.slice...)
	default:
		a.slice = append(a.slice[:pos+1], a.slice[pos:]...)
		a.slice[pos] = value
	}
}

// PopFirst - удаляет из среза элемент и возвращает его
// Если длинна массива равна 0 вернет пустое значение для типа
//
// Параметры для аргумента pos
// -1                       - добавит элемент в конец среза
// 0...n                    - добавит элемент на конкретную позици, если возможно
// n < -1 || n > len(slice) - произведет доавление в конец среза
func (a *AtomicSlice[T]) Pop(pos int) (value T) {
	a.mx.Lock()
	defer a.mx.Unlock()

	if len(a.slice) == 0 {
		return
	}

	sliceLen := len(a.slice)

	if pos < -1 || pos >= sliceLen-1 {
		pos = -1
	}

	switch pos {
	case -1:
		value = a.slice[sliceLen-1]
		a.slice = a.slice[:sliceLen-1]
	case 0:
		value = a.slice[0]
		a.slice = a.slice[1:]
	default:
		value = a.slice[pos]
		a.slice = append(a.slice[:pos], a.slice[pos+1:]...)
	}

	return value
}

// Len - возвращает длинну массива
func (a *AtomicSlice[T]) Len() int {
	a.mx.RLock()
	defer a.mx.RUnlock()
	return len(a.slice)
}

// Sort - Сортирует срез с помощью sort.Slice
func (a *AtomicSlice[T]) Sort(less func(i, j T) bool) {
	a.mx.Lock()
	defer a.mx.Unlock()

	sort.Slice(a.slice, func(i, j int) bool {
		iValue, jValue := a.slice[i], a.slice[j]
		return less(iValue, jValue)
	})
}

// Sprint - debug func
func (a *AtomicSlice[T]) Sprint() string {
	return fmt.Sprintf("%v", a.slice)
}
