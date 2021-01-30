//
// Generates files of the Sigrok Zip format (srzip).
// See https://sigrok.org/wiki/File_format:Sigrok/v2
//
// Copyright 2020-2021 Darell Tan. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the README file.
//

package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// Maximum number of samples in each file part
const SamplesLimit = 0x280000

type SrZip struct {
	zipFile *zip.Writer

	channels   []*AnalogChannel
	SampleRate uint
}

type AnalogChannel struct {
	samples uint64
	part    int
	w       io.Writer
	srzip   *SrZip
	channel int
	name    string
}

func NewSrZip(zipFile *zip.Writer) *SrZip {
	return &SrZip{zipFile: zipFile}
}

// Convenience method to specify just a filename.
func NewSrZipFile(name string) (*SrZip, error) {
	w, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return NewSrZip(zip.NewWriter(w)), nil
}

func (sr *SrZip) createFile(name, contents string) error {
	w, err := sr.zipFile.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(contents))
	return err
}

func (sr *SrZip) Close() error {
	err := sr.createFile("version", "2\n")
	if err != nil {
		return err
	}

	metadata := fmt.Sprintf(`
[device 1]
samplerate=%d
total analog=%d
`, sr.SampleRate, len(sr.channels))

	// write channel names
	for _, ch := range sr.channels {
		metadata += fmt.Sprintf("analog%d=%s\n", ch.channel, ch.name)
	}

	err = sr.createFile("metadata", metadata)
	if err != nil {
		return err
	}

	return sr.zipFile.Close()
}

func (sr *SrZip) NewAnalogChannel(name string) *AnalogChannel {
	c := &AnalogChannel{srzip: sr, name: name, channel: len(sr.channels) + 1}
	sr.channels = append(sr.channels, c)

	c.update() // initialize for the first time
	return c
}

func (c *AnalogChannel) Write(v float32) error {
	v2 := math.Float32bits(v)

	c.w.Write([]byte{ // little-endian
		byte(v2),
		byte(v2 >> 8),
		byte(v2 >> 16),
		byte(v2 >> 24),
	})

	c.samples++
	return c.update()
}

// Handles splitting of channel data into parts
func (c *AnalogChannel) update() error {
	if c.samples%SamplesLimit == 0 {
		c.part++

		name := fmt.Sprintf("analog-1-%d-%d", c.channel, c.part)
		w2, err := c.srzip.zipFile.Create(name)
		if err != nil {
			return errors.New("can't create part for analog ch " + c.name)
		}

		c.w = w2
	}
	return nil
}
