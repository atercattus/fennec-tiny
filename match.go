package fennec

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

type (
	Matcher struct {
		gaus Gaussian

		poolInts    sync.Pool
		poolValIdxs sync.Pool
		poolFloat64 sync.Pool
		poolHashes  sync.Pool
	}
)

const (
	// Максимально допустимое отклонение масштабирования от 1 (без масштабирования)
	ScaleAllowedDiff = 0.3

	// ScaleEpsilon задает порог учета временного масштабирования трека, ниже которого считаем все без учета масштаба
	ScaleEpsilon = 0.001

	// допустимое отклонение значения хеша, при котором мы еще считаем его совпавшим
	hashesDistortion = 1

	// допустимое отклонение значения времени, при котором мы еще считаем его совпавшим
	timeDistortion = 1.5

	// величина полуокресности отпимальной точки смещения, в пределах которой считаем все еще совпавшим
	offsetDistortion = 7
)

func NewMatcher() *Matcher {
	m := Matcher{}

	m.poolInts.New = func() interface{} {
		return []int{}
	}

	m.poolValIdxs.New = func() interface{} {
		return ValIdxs{}
	}

	m.poolFloat64.New = func() interface{} {
		return []float64{}
	}

	m.poolHashes.New = func() interface{} {
		return Hashes{}
	}

	return &m
}

func (m Matcher) findOptimalOffset(songA Hashes, songB Hashes) (
	offset int, cntInOffset int, sumOffs int, cntOffs int,
) {
	// предварительная проверка на отсортированность ускоряет повторное использование, но замедляет первоначальное.
	// не факт, что этот код останется в будущем. пока лишь тесты.
	if !sort.IsSorted(songA) {
		sort.Sort(songA)
	}
	if !sort.IsSorted(songB) {
		sort.Sort(songB)
	}

	swapped := false
	if len(songA) < len(songB) {
		songA, songB = songB, songA
		swapped = true
	}

	bLen := len(songB)

	offs := make(map[int32]int32)

	hashColsInOneSec := HashColsInOneSec()
	offsetInCols := int32(math.Ceil(float64(maxTimeMsDiffForTracksCompare) / 1000 * hashColsInOneSec))

	songAMaxTime, songBMaxTime := uint(0), uint(0)

	bpFrom := 0
	for _, a := range songA {
		aPP := a.ToPeakPair()
		if aPP.Bin1 == 0 || aPP.Bin2 == 0 {
			// хеши с очень низкими частотами пропускаем. малослышимый шум
			continue
		}

		if songAMaxTime < aPP.Time2() {
			songAMaxTime = aPP.Time2()
		}

		for (bpFrom < bLen) && (a.Hash > (songB[bpFrom].Hash + uint32(hashesDistortion))) {
			bpFrom++
		}

		if bpFrom >= bLen {
			break
		} else if a.Hash < songB[bpFrom].Hash {
			continue
		}

		for bp := bpFrom; (bp < bLen) && (absInt(int(songB[bp].Hash)-int(a.Hash)) <= hashesDistortion); bp++ {
			b := songB[bp]
			bPP := b.ToPeakPair()
			if bPP.Bin1 == 0 || bPP.Bin2 == 0 {
				// хеши с очень низкими частотами пропускаем. малослышимый шум
				continue
			}

			if songBMaxTime < bPP.Time2() {
				songBMaxTime = bPP.Time2()
			}

			stampA := aPP.TimeDiff
			stampB := bPP.TimeDiff

			stampDiff := float64(stampA) - float64(stampB)

			if math.Abs(stampDiff) >= timeDistortion {
				continue
			}

			tDiff := int32(a.Time) - int32(b.Time)

			if (tDiff < -offsetInCols) || (tDiff > offsetInCols) {
				continue
			}

			if n, ok := offs[tDiff]; !ok {
				offs[tDiff] = 1
			} else {
				if n++; int(n) > cntInOffset {
					cntInOffset = int(n)
					offset = int(tDiff)
				}
				offs[tDiff] = n
			}
		}
	}

	cntInOffset = 0
	for i := (offset - offsetDistortion); i < (offset + offsetDistortion); i++ {
		if o, ok := offs[int32(i)]; ok && (o >= minAllowedCnt) {
			cntInOffset += int(o)
		}
	}

	cntOffs = 0
	sumOffs = 0
	for _, v := range offs {
		if v >= minAllowedCnt {
			sumOffs += int(v)
			cntOffs++
		}
	}

	if swapped {
		offset = -offset
		songA, songB = songB, songA
	}

	return
}

func (m *Matcher) Match(songA Hashes, songB Hashes) (
	score float64, offsetInSec float64, descr string,
) {
	offset, cntInOffset, sumOffs, cntOffs := m.findOptimalOffset(songA, songB)

	if (cntOffs == 0) || (cntInOffset < minAllowedCnt) {
		return 0, 0, `` // вообще фигня, а не то, что нужно
	}

	songALen := len(songA)
	songBLen := len(songB)

	offsetInSec = float64(offset) / (float64(SampleRate) / float64(FFTWinSize/2))
	var cntInOffsetPerc float64
	if l := float64(minInt(songALen, songBLen)); l > 0 {
		cntInOffsetPerc = 100.0 * float64(cntInOffset) / l
	}

	score = float64(cntInOffset)
	scoreK := float64(1.0)

	maxOffsetInSec := maxTimeMsDiffForTracksCompare / 1000

	gaus := m.gaus.Make(maxOffsetInSec, float64(maxOffsetInSec)/3)

	offsetToIdx := int(math.Floor(offsetInSec))

	if (offsetToIdx <= -maxOffsetInSec) || (offsetToIdx >= maxOffsetInSec) {
		return 0, 0, `` // сдвиг по времени слишком большой
	} else {
		scoreK = float64(gaus[maxOffsetInSec+offsetToIdx])
	}

	score *= scoreK

	descr = fmt.Sprintf("offset: %5d cntInOffset: %5d (%5.1f%%) sumOffs: %5d cntOffs: %5d lenA: %6d lenB: %6d scoreK: %5.3f",
		offset, cntInOffset, cntInOffsetPerc, sumOffs, cntOffs, len(songA), len(songB), scoreK,
	)

	return
}
