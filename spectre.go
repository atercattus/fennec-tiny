package fennec

import (
	"github.com/mjibson/go-dsp/fft"
	"github.com/mjibson/go-dsp/window"
	"math"
	"math/cmplx"
	"sort"
)

const (
	// целевая Hz при получении PCM звука
	SampleRate = 11025

	// Размер окна FFT
	FFTWinSize = 1 << (binBits + 1)
	// Величина перекрывания окон FFT
	FFTOverlap = FFTWinSize / 2
	// Половина ширины окна FFT
	FFTHalfWinSize = FFTWinSize / 2

	// Ширина распространения пиков (см. параметр width в Gaussian.Make)
	gaussianWidth = 30.0

	// Максимум локальных пиков в одном фрейме, которые будут в итоге запомнены (ТОП самых "влиятельных")
	maxPeaksPerFrame = 6
	// Максимальное число пар пиков, которое можно образовать с каждым отдельным пиком
	maxPairsPerPeak = 2

	// максимальный lookahead по частотным диапазонам (bin'ы) про поиске пар (ограничен числом бит, выделяемых на дельту bin'ов в хеше)
	// берем как половину от binDiffMask, т.к. старший бит уходит под знак
	lookaheadBinDiffMax = binDiffMask >> 1
	// минимальный lookahead по времени при поиске пар пиков
	lookaheadTimeDiffMin = 3
	// максимальный lookahead по времени при поиске пар пиков (ограничен числом бит, выделяемых на дельту времени в хеше)
	lookaheadTimeDiffMax = timeDiffMask

	// Размерности частей хешей
	binBits      = 10
	binDiffBits  = 6 // не стоит повышать
	timeDiffBits = 6
	binMask      = (1 << binBits) - 1
	binDiffMask  = (1 << binDiffBits) - 1
	timeDiffMask = (1 << timeDiffBits) - 1

	// Минимальное число совпадений хешей при сверке двух треков, чтобы соответствующее смещение вообще бралось в рассмотрение
	minAllowedCnt = 5

	// Максимальное смещение во времени между треками, когда оно еще может считаться релевантным
	maxTimeMsDiffForTracksCompare = 10 * 60 * 1000
)

var (
	// коэффициент затухания огибающей пиков (findPeaksInSpectre)
	decayingKoeff = 0.98
)

type (
	Float float32

	PeakSpectr struct {
		Val Float
		Idx uint
	}

	Peak struct {
		Time uint // X axis
		Bin  uint // Y axis
	}

	PeakPair struct {
		Time1    uint
		Bin1     uint
		Bin2     uint
		TimeDiff uint
	}

	Hash struct {
		Time uint32
		Hash uint32
	}

	Gaussian struct {
		gaus  []float64
		n     int
		width float64
	}
)

var (
	gaussian Gaussian
)

// Сколько колонок (элементов []Hash) в одной секунде трека
func HashColsInOneSec() float64 {
	// Пока захардкодил единственные параметры
	return float64(SampleRate) / float64(FFTWinSize/2)
}

func NewPeakPair(time1, bin1, time2, bin2 uint) PeakPair {
	return PeakPair{
		Time1:    time1,
		Bin1:     bin1,
		TimeDiff: time2 - time1,
		Bin2:     bin2,
	}
}

func (pp PeakPair) ToHash() uint32 {
	bin1 := pp.Bin1 & binMask
	binDiff := (pp.Bin2 - pp.Bin1) & binDiffMask
	timeDiff := pp.TimeDiff & timeDiffMask

	hash := (((bin1 << binDiffBits) | binDiff) << timeDiffBits) | timeDiff
	return uint32(hash)
}

func (pp PeakPair) Time2() uint {
	return pp.Time1 + pp.TimeDiff
}

func (h Hash) ToPeakPair() PeakPair {
	timeDiff := uint(h.Hash & timeDiffMask)
	binDiff := uint((h.Hash >> timeDiffBits) & binDiffMask)
	bin1 := uint(((h.Hash >> timeDiffBits) >> binDiffBits) & binMask)

	d := int(binDiff)
	if (d & ((binDiffMask + 1) >> 1)) > 0 {
		d = -((binDiffMask + 1) - d)
	}

	bin2 := uint(d + int(bin1))

	return PeakPair{
		Time1:    uint(h.Time),
		Bin1:     bin1,
		Bin2:     bin2,
		TimeDiff: timeDiff,
	}
}

func (g *Gaussian) Make(n int, width float64) []float64 {
	if (g.n != n) || (g.width != width) {
		g.gaus = make([]float64, 2*n+1)
		for i := -n; i < n; i++ {
			g.gaus[i+n] = math.Exp(-0.5 * math.Pow(float64(i)/width, 2))
		}
	}

	g.n = n
	g.width = width

	return g.gaus
}

func minInt(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

func minUint(a, b uint) uint {
	if a < b {
		return a
	} else {
		return b
	}
}

func maxFloat(a, b Float) Float {
	if a > b {
		return a
	} else {
		return b
	}
}

func minFloat(a, b Float) Float {
	if a < b {
		return a
	} else {
		return b
	}
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func maxPerLine(lines [][]Float) (maxs []Float) {
	maxs = make([]Float, len(lines))
	for i, line := range lines {
		max := float64(0)
		for _, val := range line {
			max = math.Max(max, float64(val))
		}
		maxs[i] = Float(max)
	}
	return
}

func maximumFloat(a []Float, b []Float) {
	l := minInt(len(a), len(b))
	for i := 0; i < l; i++ {
		if a[i] < b[i] {
			a[i] = b[i]
		}
	}
}

func fading(vec []Float, factor Float) {
	for i := range vec {
		vec[i] *= factor
	}
}

func buildSpectre(wave []Float) (spectre [][]Float) {
	waveLen := len(wave)
	if waveLen == 0 {
		return
	}

	winFunc := window.Hann(FFTWinSize + 2)[1 : FFTWinSize+1]

	spectre = make([][]Float, FFTHalfWinSize+1) // rows x cols

	win := make([]float64, FFTWinSize)
	winZeroes := make([]float64, FFTWinSize)

	stride := FFTWinSize - FFTOverlap
	winCnt := (waveLen + stride - 1) / stride
	for winIdx, offs := 0, 0; winIdx < winCnt; winIdx, offs = winIdx+1, offs+stride {
		idx := minInt(waveLen, offs+FFTWinSize)
		if idx < (offs + FFTWinSize) {
			copy(win, winZeroes)
		}

		for i := offs; i < idx; i++ {
			win[i-offs] = float64(wave[i])
		}

		for i, w := range winFunc {
			win[i] *= w
		}

		line := fft.FFTReal(win)
		for i := 0; i < FFTHalfWinSize+1; i++ {
			win[i] = cmplx.Abs(line[i])
		}

		for i, mag := range win[0 : FFTHalfWinSize+1] {
			spectre[i] = append(spectre[i], Float(mag))
		}
	}

	spectreMin, spectreMax := Float(math.MaxFloat32), Float(0.0)
	for _, line := range spectre {
		for _, mag := range line {
			spectreMax = maxFloat(spectreMax, mag)
			spectreMin = minFloat(spectreMin, mag)
		}
	}

	if spectreMax < 1e-6 {
		panic(`Zero signal`)
	}

	minMag := spectreMax / 1e6
	spectreMean, cnt := float64(0), 0
	for y, line := range spectre {
		for x, magRaw := range line {
			if magRaw < minMag {
				magRaw = minMag
			}
			mag := math.Log(float64(magRaw))
			//mag = math.Max(0, mag) // раскомментировать, чтобы получить спектрограмму как в презентации
			spectre[y][x] = Float(mag)

			spectreMean += mag
			cnt++
		}
	}

	spectreMean /= float64(cnt)

	for y, line := range spectre {
		for x := range line {
			spectre[y][x] -= Float(spectreMean)
		}
	}

	spectre = spectre[:len(spectre)-1]

	return
}

func findPeaksInSpectre(spectre [][]Float) (peakList []Peak) {
	peaks := scanForPeaks(spectre, Float(decayingKoeff))
	peaks = filterPeaks(spectre, peaks, Float(decayingKoeff))

	srows, scols := len(spectre), len(spectre[0])
	for x := 0; x < scols; x++ {
		for y := 0; y < srows; y++ {
			if peaks[y][x] > 0 {
				peakList = append(peakList, Peak{Time: uint(x), Bin: uint(y)})
			}
		}
	}

	return
}

func findPeaks(wave []Float) (peakList []Peak, spectre [][]Float) {
	spectre = buildSpectre(wave)
	if len(spectre) == 0 {
		return
	}

	return findPeaksInSpectre(spectre), spectre
}

func scanForPeaks(spectre [][]Float, shadingCoeff Float) [][]int {
	numRows, numCols := len(spectre), len(spectre[0])

	scolsThresh := minInt(10, numCols)

	lines := make([][]Float, numRows)
	for y, line := range spectre {
		lines[y] = line[0:scolsThresh]
	}

	maximumInLines := maxPerLine(lines)

	thresh := spreadPeaksInVector(maximumInLines, gaussianWidth)

	peaks := make([][]int, numRows)
	for y := range spectre {
		peaks[y] = make([]int, numCols)
	}

	scol := make([]Float, numRows)

	for col := 0; col < numCols; col++ {
		for y := 0; y < numRows; y++ {
			scol[y] = spectre[y][col]
		}

		var peaksPositions []int
		for i, isLocMax := range locMax(scol) {
			if isLocMax && (scol[i] > thresh[i]) {
				peaksPositions = append(peaksPositions, i)
			}
		}

		if len(peaksPositions) > 0 {
			var valsPeaks PeakSpectrSlice
			for _, idx := range peaksPositions {
				valsPeaks = append(valsPeaks, PeakSpectr{Idx: uint(idx), Val: scol[idx]})
			}

			sort.Sort(valsPeaks)

			if len(valsPeaks) > maxPeaksPerFrame {
				valsPeaks = valsPeaks[0:maxPeaksPerFrame]
			}
			for _, valsPeak := range valsPeaks {
				peakPos := valsPeak.Idx
				peak := PeakSpectr{Idx: peakPos, Val: scol[peakPos]}
				thresh = spreadPeaks([]PeakSpectr{peak}, 0, gaussianWidth, thresh)

				peaks[peakPos][col] = 1
			}
		}

		fading(thresh, Float(shadingCoeff))
	}

	return peaks
}

func filterPeaks(spectre [][]Float, peaks [][]int, shadingCoeff Float) [][]int {
	numRows, numCols := len(spectre), len(spectre[0])

	lastCol := make([]Float, numRows)
	for y := 0; y < numRows; y++ {
		lastCol[y] = spectre[y][numCols-1]
	}
	thresh := spreadPeaksInVector(lastCol, gaussianWidth)

	for col := numCols; col > 0; col-- {
		var colPeaks PeakSpectrSlice
		for y := 0; y < numRows; y++ {
			if peaks[y][col-1] > 0 {
				colPeaks = append(colPeaks, PeakSpectr{Idx: uint(y), Val: spectre[y][col-1]})
			}
		}

		sort.Sort(colPeaks)

		for _, peak := range colPeaks {
			if peak.Val > thresh[peak.Idx] {
				thresh = spreadPeaks([]PeakSpectr{peak}, 0, gaussianWidth, thresh)
				if col < numCols {
					peaks[peak.Idx][col] = 0
				}
			} else {
				peaks[peak.Idx][col-1] = 0
			}
		}

		fading(thresh, shadingCoeff)
	}

	return peaks
}

func locMaxIndices(vec []Float) []int {
	neighbours := locMax(vec)

	var idxs []int
	for i, isLocMax := range neighbours {
		if isLocMax {
			idxs = append(idxs, i)
		}
	}

	return idxs
}

func locMax(vec []Float) []bool {
	l := len(vec)
	neighbours := make([]bool, l)

	neighbours[0] = vec[0] >= vec[1]
	neighbours[l-1] = vec[l-1] >= vec[l-2]
	for i := 1; i < l-1; i++ {
		neighbours[i] = (vec[i-1] <= vec[i]) && (vec[i] >= vec[i+1])
	}

	return neighbours
}

func spreadPeaksInVector(vector []Float, width float64) []Float {
	var peaks []PeakSpectr

	for _, idx := range locMaxIndices(vector) {
		peaks = append(peaks, PeakSpectr{Idx: uint(idx), Val: vector[idx]})
	}

	return spreadPeaks(peaks, len(vector), width, nil)
}

func spreadPeaks(peaks []PeakSpectr, numPoints int, width float64, base []Float) []Float {
	if base != nil {
		numPoints = len(base)
	}
	vec := make([]Float, numPoints)
	if base != nil {
		copy(vec, base)
	}

	gaus := gaussian.Make(numPoints, width)

	gausVal := make([]Float, numPoints)
	for _, peak := range peaks {
		for i := 0; i < numPoints; i++ {
			gausVal[i] = peak.Val * Float(gaus[uint(i+numPoints)-peak.Idx])
		}
		maximumFloat(vec, gausVal)
	}

	return vec
}

func PeaksToPairs(peaks []Peak) (pairs []PeakPair) {
	if len(peaks) == 0 {
		return
	}

	timeCnt := peaks[len(peaks)-1].Time + 1

	peaksAt := make([][]uint, timeCnt)
	for _, peak := range peaks {
		peaksAt[peak.Time] = append(peaksAt[peak.Time], peak.Bin)
	}

	for time1 := uint(0); time1 < timeCnt; time1++ {
	pairsLoop:
		for _, bin1 := range peaksAt[time1] {
			pairsFromThisPeak := 0
			lastTime2 := minUint(timeCnt, time1+lookaheadTimeDiffMax)
			for time2 := time1 + lookaheadTimeDiffMin; time2 < lastTime2; time2++ {
				for _, bin2 := range peaksAt[time2] {
					if absInt(int(bin2)-int(bin1)) < lookaheadBinDiffMax {
						pair := NewPeakPair(time1, bin1, time2, bin2)
						pairs = append(pairs, pair)

						if pairsFromThisPeak++; pairsFromThisPeak >= maxPairsPerPeak {
							continue pairsLoop
						}
					}
				}
			}
		}
	}

	return
}

func PeakPairsToHashes(pairs []PeakPair) (hashes Hashes) {
	hashes = make([]Hash, len(pairs))

	for i, pair := range pairs {
		hashes[i] = Hash{Time: uint32(pair.Time1), Hash: pair.ToHash()}
	}

	return
}

func FindHashes(peaks []Peak) (hashes Hashes) {
	return PeakPairsToHashes(PeaksToPairs(peaks))
}
