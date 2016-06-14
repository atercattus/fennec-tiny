package fennec

import (
	"io"
)

const (
	// signed int16 to float -1..1
	int16ToFloat = Float(1 << 15)
)

func ReadMp3(path string) (pcm []Float, err error) {
	rd, err := NewMP3Reader(path, SampleRate, 16)
	if err != nil {
		return nil, err
	}
	defer rd.Close()

	var frame []int16
	for {
		frame, err = rd.ReadFrame(frame)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		for _, f := range frame {
			pcm = append(pcm, Float(f)/int16ToFloat)
		}
	}

	return pcm, nil
}

func GenPeaksFromMp3(path string) ([]Peak, error) {
	pcm, err := ReadMp3(path)
	if err != nil {
		return nil, err
	}

	spectre := buildSpectre(pcm)

	return findPeaksInSpectre(spectre), nil
}

func GenPeaksFromMp3WithSpectre(path string) ([]Peak, [][]Float, error) {
	pcm, err := ReadMp3(path)
	if err != nil {
		return nil, nil, err
	}

	spectre := buildSpectre(pcm)

	return findPeaksInSpectre(spectre), spectre, nil
}
