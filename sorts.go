package fennec

type (
	// PeakSpectrSlice сортирует по убыванию Val
	PeakSpectrSlice []PeakSpectr

	// Hashes сортирует по возрастанию значения поля Hash
	Hashes []Hash

	// HashesByTime сортирует по возрастанию времени, а при равных - по возрастанию Hash.TimeDiff
	HashesByTime []Hash

	ValIdx struct {
		Val uint32
		Idx uint32
	}
	// ValIdxs сортирует по возрастанию Val, а при равных - по убыванию Idx
	ValIdxs []ValIdx
)

func (p PeakSpectrSlice) Len() int           { return len(p) }
func (p PeakSpectrSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p PeakSpectrSlice) Less(i, j int) bool { return p[i].Val > p[j].Val }

func (h Hashes) Len() int      { return len(h) }
func (h Hashes) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h Hashes) Less(i, j int) bool {
	return (h[i].Hash < h[j].Hash) || (h[i].Hash == h[j].Hash && h[i].Time < h[j].Time)
}

func (h HashesByTime) Len() int      { return len(h) }
func (h HashesByTime) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h HashesByTime) Less(i, j int) bool {
	if dTime := int64(h[i].Time) - int64(h[j].Time); dTime < 0 {
		return true
	} else if dTime > 0 {
		return false
	}
	// dTime == 0
	return h[i].ToPeakPair().TimeDiff < h[j].ToPeakPair().TimeDiff
}

func (a ValIdxs) Len() int      { return len(a) }
func (a ValIdxs) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ValIdxs) Less(i, j int) bool {
	if d := int64(a[i].Val) - int64(a[j].Val); d < 0 {
		return true
	} else if d > 0 {
		return false
	}

	return a[i].Idx < a[j].Idx
}
