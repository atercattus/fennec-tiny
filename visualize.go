package fennec

import (
	"github.com/llgcode/draw2d/draw2dimg"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func VisualizeSpectre(spectre [][]Float, peaks []Peak, hashes []Hash) image.Image {
	numRows := len(spectre)
	var numCols int
	if numRows > 0 {
		numCols = len(spectre[0])
	}

	minInSpectre, maxInSpectre := math.MaxFloat64, float64(0)
	for y := 0; y < numRows; y++ {
		for x := 0; x < numCols; x++ {
			minInSpectre = math.Min(minInSpectre, float64(spectre[y][x]))
			maxInSpectre = math.Max(maxInSpectre, float64(spectre[y][x]))
		}
	}
	diffM := maxInSpectre - minInSpectre

	img := image.NewRGBA(image.Rect(0, 0, numCols, numRows))
	gCtx := draw2dimg.NewGraphicContext(img)

	if len(spectre) > 0 {
		for row := 0; row < numRows; row++ {
			for col := 0; col < numCols; col++ {
				vol := spectre[row][col]
				vol -= Float(minInSpectre)

				clr := uint8((255 * vol / Float(diffM)) / 2)
				pix := color.RGBA{clr, clr, clr, 255}

				x := col
				y := numRows - row - 1

				img.Set(x, y, pix)
			}
		}
	}

	if len(peaks) > 0 {
		for _, peak := range peaks {
			row := int(peak.Bin)
			col := int(peak.Time)

			x := float64(col)
			y := float64(numRows - row - 1)

			dist := float64(1.0)
			gCtx.MoveTo(x-dist, y-dist)
			gCtx.LineTo(x+dist, y-dist)
			gCtx.LineTo(x+dist, y+dist)
			gCtx.LineTo(x-dist, y+dist)
			gCtx.LineTo(x-dist, y-dist)
			cr := uint8(255)
			cg := uint8(255)
			cb := uint8(255)
			gCtx.SetFillColor(color.RGBA{cr, cg, cb, 255})
			gCtx.Fill()
		}
	}

	if len(hashes) > 0 {
		for _, hash := range hashes {
			pp := hash.ToPeakPair()
			bin1Row := int(pp.Bin1)
			bin1Col := int(pp.Time1)

			bin2Row := int(pp.Bin2)
			bin2Col := int(pp.Time1 + pp.TimeDiff)

			x1 := float64(bin1Col)
			y1 := float64(numRows - bin1Row - 1)

			x2 := float64(bin2Col)
			y2 := float64(numRows - bin2Row - 1)

			gCtx.MoveTo(x1, y1)
			gCtx.LineTo(x2, y2)
			cr := uint8(55 + int64(x1/2)%200)
			cg := uint8(55 + int64(y1/2)%200)
			cb := uint8(155)
			gCtx.SetStrokeColor(color.RGBA{cr, cg, cb, 255})
			gCtx.Stroke()
		}
	}

	return img
}

func SaveToPng(img image.Image, path string) error {
	if img == nil {
		return nil
	}

	if frameImgFd, err := os.Create(path); err != nil {
		return err
	} else {
		defer frameImgFd.Close()
		if err := png.Encode(frameImgFd, img); err != nil {
			return err
		}

		return nil
	}
}
