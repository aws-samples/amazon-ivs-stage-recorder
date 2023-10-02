package main

import (
	"fmt"
	"os"
	"time"

	"github.com/at-wat/ebml-go/webm"
	"github.com/jech/samplebuilder"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
)

func writeRTPPacketsToMKV(s *samplebuilder.SampleBuilder, track *webrtc.TrackRemote, blockWriter webm.BlockWriter) {
	var totalDuration time.Duration
	for {
		rtpPacket, _, err := track.ReadRTP()
		if err != nil {
			panic(err)
		}

		s.Push(rtpPacket)

		for {
			sample := s.Pop()
			if sample == nil {
				break
			}

			if track.Kind() == webrtc.RTPCodecTypeAudio {
				fmt.Println("write")
			}

			totalDuration += sample.Duration
			if _, err := blockWriter.Write(true, int64(totalDuration/time.Millisecond), sample.Data); err != nil {
				panic(err)
			}

		}
	}
}

func startAudioWriter(track *webrtc.TrackRemote, audioWriterChan chan webm.BlockWriter) {
	writeRTPPacketsToMKV(samplebuilder.New(10, &codecs.OpusPacket{}, 48000), track, <-audioWriterChan)
}

func startVideoWriter(track *webrtc.TrackRemote, audioWriterChan chan webm.BlockWriter) {
	file, err := os.OpenFile("out.mkv", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		panic(err)
	}
	fmt.Println("Starting new file 'out.mkv'")

	var (
		depacketizer  rtp.Depacketizer = &codecs.VP8Packet{}
		videoMimeType string           = "V_VP8"
	)
	if track.Codec().MimeType == webrtc.MimeTypeH264 {
		videoMimeType = "V_MPEG4/ISO/AVC"
		depacketizer = &codecs.H264Packet{}
	}

	s := samplebuilder.New(150, depacketizer, 90000)

	webmWriters, err := webm.NewSimpleBlockWriter(file,
		[]webm.TrackEntry{
			{
				Name:            "Audio",
				TrackNumber:     1,
				TrackUID:        12345,
				CodecID:         "A_OPUS",
				TrackType:       2,
				DefaultDuration: 20000000,
				Audio: &webm.Audio{
					SamplingFrequency: 48000.0,
					Channels:          2,
				},
			}, {
				Name:            "Video",
				TrackNumber:     2,
				TrackUID:        67890,
				CodecID:         videoMimeType,
				TrackType:       1,
				DefaultDuration: 33333333,
			},
		})
	if err != nil {
		panic(err)
	}

	audioWriterChan <- webmWriters[0]
	writeRTPPacketsToMKV(s, track, webmWriters[1])

}
