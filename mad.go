package fennec

/*
#include <sys/mman.h>
#include <mad.h>
#include <string.h>
#include <stdint.h>
#include <stdlib.h>

#cgo LDFLAGS: -lmad

typedef struct {
	struct mad_stream madStream;
	struct mad_frame  madFrame;
	struct mad_synth  madSynth;
} mad_mp3reader;

int my_mad_recoverable(int error) {
	return MAD_RECOVERABLE(error);
}

size_t calcRemainInMadStream(struct mad_stream *stream) {
	return stream->bufend - stream->next_frame;
}

void my_mad_close_reader(mad_mp3reader *r) {
	mad_synth_finish(&r->madSynth);
	mad_frame_finish(&r->madFrame);
	mad_stream_finish(&r->madStream);
	free(r);
}

mad_mp3reader *my_mad_open_reader(char *buf, size_t file_size) {
	mad_mp3reader *r = (mad_mp3reader*)malloc(sizeof(mad_mp3reader));
	memset(r, 0, sizeof(mad_mp3reader));

	mad_stream_init(&r->madStream);
	mad_frame_init(&r->madFrame);
	mad_synth_init(&r->madSynth);

	mad_stream_buffer(&r->madStream, buf, file_size);

	return r;
}

int my_mad_frame_decode(mad_mp3reader *r) {
	return mad_frame_decode(&r->madFrame, &r->madStream);
}

*/
import "C"

import (
	"errors"
	"io"
	"math"
	"os"
	"unsafe"
)

var (
	ErrWrongParams = errors.New(`Wrong params`)
	ErrMmapFail    = errors.New(`mmap fail`)
)

type (
	mp3Reader struct {
		fd   *os.File
		size int64

		sampleRate int

		reader    *C.mad_mp3reader
		mmap      unsafe.Pointer
		lastFrame []byte

		accumBuf []int16
	}

	errMadUnrecover struct {
		err string
	}
)

// madScale по аналогии с "madplay audio.c audio_linear_round()"
func madScale(smpl C.mad_fixed_t) int16 {
	sample := int32(smpl)

	shift := uint(C.MAD_F_FRACBITS - 16)
	min, max := int32(-C.MAD_F_ONE), int32(C.MAD_F_ONE-1)

	sample += (1 << shift)

	if sample > max {
		sample = max
	} else if sample < min {
		sample = min
	}

	sample >>= (shift + 1)

	return int16(sample)
}

func mergeChannels(ch1, ch2 int16) int16 {
	return int16((int32(ch1) + int32(ch2)) >> 1)
}

func (err errMadUnrecover) Error() string {
	return `Unecoverable mad decoding error: ` + err.err
}

func NewMP3Reader(path string, sampleRate int, bits int) (*mp3Reader, error) {
	if bits != 16 {
		return nil, ErrWrongParams
	} else if (sampleRate < 11025) || (sampleRate%11025 != 0) { // пример без интерполирования
		return nil, ErrWrongParams
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	rd := &mp3Reader{
		fd:         fd,
		size:       fi.Size(),
		sampleRate: sampleRate,
	}

	rd.openMp3()

	return rd, nil
}

func (mp3r *mp3Reader) Close() error {
	if mp3r.fd == nil {
		return ErrWrongParams
	}
	mp3r.fd.Close()
	mp3r.fd = nil

	C.my_mad_close_reader(mp3r.reader)

	C.munmap(mp3r.mmap, C.size_t(mp3r.size))

	if mp3r.lastFrame != nil {
		mp3r.lastFrame = nil
	}

	return nil
}

// ReadFrame читает один PCM фрейм из файла
// buf используется как буфер под ответ, чтобы не выделять память при каждом вызове.
// На случай расширения буфера (если размера не хватило) функция возвращает новый буфер (или тот же).
func (mp3r *mp3Reader) ReadFrame(buf []int16) ([]int16, error) {
	stream := &mp3r.reader.madStream
	frame := &mp3r.reader.madFrame

	for {
		if C.mad_frame_decode(frame, stream) != 0 {
			if C.my_mad_recoverable(C.int(stream.error)) != 0 {
				if mp3r.lastFrame != nil {
					break
				} else {
					continue
				}
			} else if stream.error == C.MAD_ERROR_BUFLEN {
				if mp3r.lastFrame != nil {
					return nil, io.EOF
				}

				remaining := int(C.calcRemainInMadStream(stream))
				lastFrameLen := remaining + C.MAD_BUFFER_GUARD

				mp3r.lastFrame = make([]byte, lastFrameLen)
				lastFramePtr := unsafe.Pointer(&mp3r.lastFrame[0])
				C.memmove(lastFramePtr, unsafe.Pointer(stream.next_frame), C.size_t(remaining))

				C.mad_stream_init(stream)
				C.mad_stream_buffer(stream, (*C.uchar)(lastFramePtr), C.ulong(lastFrameLen))

				continue
			} else {
				cstr := C.mad_stream_errorstr(stream)
				return nil, errMadUnrecover{C.GoString(cstr)}
			}
		}

		C.mad_synth_frame(&mp3r.reader.madSynth, frame)
		return mp3r.buildCurrentFrame(buf, false), nil
	}

	return nil, io.EOF
}

func (mp3r *mp3Reader) openMp3() error {
	mp3r.mmap = C.mmap(unsafe.Pointer(uintptr(0)), C.size_t(mp3r.size), C.PROT_READ, C.MAP_PRIVATE, C.int(mp3r.fd.Fd()), C.off_t(0))
	if mp3r.mmap == nil {
		return ErrMmapFail
	}

	mp3r.reader = C.my_mad_open_reader((*C.char)(mp3r.mmap), C.size_t(mp3r.size))

	return nil
}

func (mp3r *mp3Reader) buildCurrentFrame(buf []int16, forceMono bool) []int16 {
	pcm := &(mp3r.reader.madSynth.pcm)

	srcSampleRate := int(pcm.samplerate)
	srcSamplesCnt := int(pcm.length)
	srcChannelsCnt := int(pcm.channels)

	leftCh := pcm.samples[0]
	leftChIdx := 0
	rightCh := pcm.samples[1]
	rightChIdx := 0

	buf = buf[0:0]

	sampleRateKoeff := srcSampleRate / mp3r.sampleRate
	if cap(mp3r.accumBuf) < sampleRateKoeff {
		mp3r.accumBuf = make([]int16, sampleRateKoeff)
	}

	var (
		mixAvg, leftAvg, rightAvg int64
	)

	accumIdx := 0
	for sampleIdx := 0; sampleIdx < srcSamplesCnt; sampleIdx++ {
		sample := madScale(leftCh[leftChIdx])
		leftChIdx++
		leftAvg += int64(sample)

		if !forceMono && (srcChannelsCnt == 2) {
			sample2 := madScale(rightCh[rightChIdx])
			rightChIdx++
			rightAvg += int64(sample2)
			sample = mergeChannels(sample, sample2)
		}

		if sampleRateKoeff > 1 {
			mp3r.accumBuf[accumIdx] = sample
			if accumIdx++; accumIdx == sampleRateKoeff {
				accumIdx = 0

				sampleAccum := 0
				for i := 0; i < sampleRateKoeff; i++ {
					sampleAccum += int(mp3r.accumBuf[i])
				}
				sample = int16(sampleAccum / sampleRateKoeff)
			} else {
				continue
			}
		}

		buf = append(buf, sample)

		mixAvg += int64(sample)
	}

	if !forceMono && (len(buf) > 0) {
		if avg := math.Abs(float64(mixAvg) / float64(len(buf))); avg < 1 {
			// Если среднее значение вышло ни то ни се, а отдельные L/R каналы почти что зеркально противоположны,
			//   то просто берем в качестве результа просто один из каналов (повторный проход по фрейму).
			avgL := float64(leftAvg) / float64(srcSamplesCnt)
			avgR := float64(rightAvg) / float64(srcSamplesCnt)
			if (math.Abs(avgL+avgR) < 1) && (math.Abs(avgL) >= 1) {
				return mp3r.buildCurrentFrame(buf, true)
			}
		}
	}

	return buf
}
