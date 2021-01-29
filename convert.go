//
// Converts binary files produced by Siglent SDS-1000 series scopes.
// The file can be saved onto a thumbdrive or via the web UI using the
// "Save/Recall" feature on the oscilloscope.
//
// Copyright 2020 Darell Tan. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the README file.
//

package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
)

type Header struct {
	_ [16]byte

	Scale1, _, Scale2, _, Scale3, _, Scale4, _     float64
	Offset1, _, Offset2, _, Offset3, _, Offset4, _ float64
}

var (
	use_10x     = flag.Bool("10x", false, "apply 10x multiplier")
	writeRaw    = flag.Bool("raw", false, "write a raw values file")
	applyOffset = flag.Bool("offset", true, "apply offset to values")
	startOffset = flag.Float64("start-at", 0, "starting offset (in milliseconds) to process from")
	decimate    = flag.Int("decimate", 1, "apply decimation factor to waveform")
)

//////////////////////////////////////////////////

// Writer for values
type ValueWriter interface {
	Write(v float32) error
	Close() error
}

type FileValueWriter struct {
	file *os.File
	w    io.Writer
}

func NewFileValueWriter(name string) (*FileValueWriter, error) {
	outfile, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Printf("cant create output file: %v\n", err)
		return nil, err
	}

	wbuf := bufio.NewWriter(outfile)
	return &FileValueWriter{file: outfile, w: wbuf}, nil
}

func (f *FileValueWriter) Write(v float32) error { return binary.Write(f.w, binary.LittleEndian, v) }
func (f *FileValueWriter) Close() error          { return f.file.Close() }

type SrValueWriter struct {
	file *SrZip
	ch   *AnalogChannel
}

func (s *SrValueWriter) Write(v float32) error { return s.ch.Write(v) }
func (s *SrValueWriter) Close() error          { return s.file.Close() }

//////////////////////////////////////////////////

func main() {
	flag.Parse()
	if *decimate < 1 {
		fmt.Println("decimation factor cannot be less than 1")
		return
	}

	fname := flag.Arg(0)
	fmt.Printf("fname %s\n", fname)

	file, err := os.Open(fname)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	hdr := Header{}
	if err := binary.Read(file, binary.LittleEndian, &hdr); err != nil {
		fmt.Printf("cannot read header: %v\n", err)
		return
	}

	fmt.Printf("%+v\n", hdr)

	if _, err := file.Seek(0xF4, 0 /*SeekStart*/); err != nil {
		fmt.Printf("cant seek: %v\n", err)
		return
	}

	dataSpec := struct {
		Points     uint32  // number of points
		SampleRate float64 // samples per second
	}{}

	if err := binary.Read(file, binary.LittleEndian, &dataSpec); err != nil {
		fmt.Printf("cannot read specs: %v\n", err)
		return
	}

	fmt.Printf("%+v\n", dataSpec)

	// seek to data
	if _, err := file.Seek(0x800, 0 /*SeekStart*/); err != nil {
		fmt.Printf("cant seek to data: %v\n", err)
		return
	}

	var output ValueWriter
	if *writeRaw {
		output, err = NewFileValueWriter(fname + "-raw.bin")
		if err != nil {
			panic(err)
		}
	} else {
		sr, err := NewSrZipFile(fname + ".sr")
		if err != nil {
			fmt.Printf("cant create srzip: %v\n", err)
			return
		}

		sr.SampleRate = uint(dataSpec.SampleRate / float64(*decimate))
		ch := sr.NewAnalogChannel("CH 1")

		output = &SrValueWriter{sr, ch}
	}

	// pre-process probe multplier
	if *use_10x {
		hdr.Scale1 = 10 * hdr.Scale1
		hdr.Offset1 = 10 * hdr.Offset1
	}

	fmt.Printf("scale: %f\noffset: %f\n", hdr.Scale1, hdr.Offset1)

	// should we apply offset voltage? (used for debugging)
	offset := float64(0)
	if *applyOffset {
		offset = hdr.Offset1
	}

	decimateSkip := uint(*decimate) - 1

	rbuf := bufio.NewReader(file)
	i := uint(0)

	// discard points until starting offset
	if *startOffset > 0 {
		startPoint := uint(*startOffset * dataSpec.SampleRate / 1000 /*ms*/)
		i = startPoint
		if _, err := rbuf.Discard(int(startPoint)); err != nil {
			fmt.Printf("cannot skip to start offset: %v\n", err)
			return
		}
	}

	for ; i < uint(dataSpec.Points); i++ {
		v, err := rbuf.ReadByte()
		if err != nil {
			panic(err)
		}

		// perform decimation
		if decimateSkip > 0 && i+decimateSkip < uint(dataSpec.Points) {
			i += decimateSkip
			_, err = rbuf.Discard(int(decimateSkip))
			if err != nil {
				panic(err)
			}
		}

		v2 := float64(int(v)-128) * hdr.Scale1 * 10.7 / 256
		v2 -= offset

		// writes converted raw values directly to output file
		err = output.Write(float32(v2))
		if err != nil {
			panic(err)
		}
	}

	if err := output.Close(); err != nil {
		panic(err)
	}
}
