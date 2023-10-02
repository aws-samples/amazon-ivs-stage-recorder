package main

import (
	"fmt"
	"os"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/webrtc/v3"
)

func main() {
	if len(os.Args) != 3 {
		panic("IVSStageSaver requires a Token and ID")
	}
	bearerToken := os.Args[1]
	participantId := os.Args[2]

	// Extract details from IVS Token to make WHEP request
	whepURL, turnRequired, subscriberSendAudio, err := extractTokenDetails(bearerToken)
	if err != nil {
		panic(fmt.Sprintf("Failed decode IVS Stages Token %s", err.Error()))
	}

	_, err = createPeerConnection(whepURL+"/subscribe/"+participantId, bearerToken, turnRequired, func(peerConnection *webrtc.PeerConnection) error {

		// Legacy IVS Stages require audio to be sent to receive audio
		if subscriberSendAudio {
			audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
			if err != nil {
				panic(fmt.Sprintf("Failed to NewTrackLocalStaticSample %s", err.Error()))
			}
			go sendSilentAudio(audioTrack)

			if _, err = peerConnection.AddTrack(audioTrack); err != nil {
				panic(fmt.Sprintf("Failed to AddTrack %s", err.Error()))
			}
		} else {
			if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
				panic(fmt.Sprintf("Failed to AddTransceiverFromKind %s", err.Error()))
			}
		}

		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			panic(fmt.Sprintf("Failed to AddTransceiverFromKind %s", err.Error()))
		}

		peerConnection.OnConnectionStateChange(func(connectionState webrtc.PeerConnectionState) {
			fmt.Printf("Connection State has changed %s \n", connectionState.String())
		})

		// Save incoming tracks to disk
		audioWriterChan := make(chan webm.BlockWriter, 1)
		peerConnection.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			if t.Kind() == webrtc.RTPCodecTypeVideo {
				startVideoWriter(t, audioWriterChan)
			} else {
				startAudioWriter(t, audioWriterChan)
			}
		})

		return nil
	})

	if err != nil {
		panic(fmt.Sprintf("Failed to createPeerConnection %s", err.Error()))
	}

	select {}
}
