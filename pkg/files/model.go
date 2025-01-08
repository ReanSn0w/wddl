package files

type Stat struct {
	ID                string
	FileName          string
	Count             int64
	Done              int64
	SkipBytes         int64
	LastPartitionSize int64
}

func (ps *Stat) GetID() string {
	return ps.ID
}

func (ps *Stat) GetFileName() string {
	return ps.FileName
}

func (ps *Stat) CompletePercent() float64 {
	if ps.Count == 0 {
		return 0
	}
	return float64(ps.Done) / float64(ps.Count) * 100
}

func (ps *Stat) IsComplete() bool {
	return ps.Done == ps.Count
}
