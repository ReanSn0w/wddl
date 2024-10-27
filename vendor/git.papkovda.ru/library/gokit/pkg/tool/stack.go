package tool

const (
	StackModeFIFO StackMode = iota
	StackModeFILO
)

type StackMode int

func NewStack[T any](mode StackMode) *Stack[T] {
	return &Stack[T]{
		storage: NewAtomicSlice[T](),
		mode:    mode,
	}
}

type Stack[T any] struct {
	storage *AtomicSlice[T]
	mode    StackMode
}

func (s *Stack[T]) Push(val T) {
	s.storage.Push(-1, val)
}

func (s *Stack[T]) Pop() (val T) {
	switch s.mode {
	case StackModeFIFO:
		return s.storage.Pop(0)
	default:
		return s.storage.Pop(-1)
	}
}

func (s *Stack[T]) Len() int {
	return s.storage.Len()
}

func (s *Stack[T]) Sprint() string {
	return s.storage.Sprint()
}
