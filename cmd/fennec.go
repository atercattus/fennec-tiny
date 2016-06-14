package main

import (
	"flag"
	"fmt"
	fennec "github.com/atercattus/fennec-tiny"
	"math"
	"os"
	"path"
)

var (
	argv struct {
		withSpectre bool
		withPeaks   bool
		withPairs   bool
	}
)

func init() {
	flag.BoolVar(&argv.withSpectre, `spectre`, false, `Write spectre PNGs (save in current directory)`)
	flag.BoolVar(&argv.withPeaks, `peaks`, false, `Visualize peaks on spectre`)
	flag.BoolVar(&argv.withPairs, `pairs`, false, `Visualize peaks pairs on spectre`)
	flag.Parse()
	if argv.withPeaks || argv.withPairs {
		argv.withSpectre = true
	}
}

func loadHashes(p string) (fennec.Hashes, error) {
	if peaks, spectre, err := fennec.GenPeaksFromMp3WithSpectre(p); err != nil {
		return nil, err
	} else {
		hashes := fennec.FindHashes(peaks)

		if argv.withSpectre {
			hashesDraw := hashes
			if !argv.withPairs {
				hashesDraw = nil
			}
			if !argv.withPeaks {
				peaks = nil
			}
			fennec.SaveToPng(fennec.VisualizeSpectre(spectre, peaks, hashesDraw), path.Base(p)+`.png`)
		}

		return hashes, nil
	}
}

func main() {
	if len(flag.Args()) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [params] track1.mp3 track2.mp3\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	var (
		hashes1 fennec.Hashes
		hashes2 fennec.Hashes
		err     error
	)

	if hashes1, err = loadHashes(flag.Arg(0)); err != nil {
		panic(err)
	} else if hashes2, err = loadHashes(flag.Arg(1)); err != nil {
		panic(err)
	}

	l := len(hashes1)
	if l2 := len(hashes2); l2 < l {
		l = l2
	}
	if l == 0 {
		fmt.Println(`0 (no data)`)
		return
	}

	score, offset, _ := fennec.NewMatcher().Match(hashes1, hashes2)
	eq := 100 * float32(score) / float32(l) // самый примитивный вариант подсчета итоговой похожести
	eq = eq2perc(eq)

	fmt.Printf("%.3f (offset %.2f sec)\n", eq, offset)
}

func eq2perc(eq float32) float32 {
	if eq <= 0 {
		return 0
	}

	e := float64(eq) / 100
	perc := math.Pow(e, 1.0/3.0) - 0.3
	perc = math.Min(1, 1.4*math.Max(0, perc))
	return float32(100 * perc)
}
