package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/pion/dtls/v2"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	errExtractClaims   = errors.New("Could not extract claims from token")
	errWHIPURLNotFound = errors.New("whip_url was not found in token")
	errVersionNotFound = errors.New("version was not found in token")
	errVersionInvalid  = errors.New("version was found in token but is invalid")
)

const (
	versionFlagMandatoryTurn           = 0b00000000000000000000000000000001
	versionFlagAudioRequiredForReceive = 0b00000000000000000000000000000010
)

func extractTokenDetails(bearerToken string) (whipURL string, turnRequired, subscriberSendAudio bool, err error) {
	token, _ := jwt.Parse(bearerToken, func(token *jwt.Token) (interface{}, error) {
		return "", nil
	})

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		err = errExtractClaims
		return
	}

	whipURL, ok = claims["whip_url"].(string)
	if !ok || whipURL == "" {
		err = errWHIPURLNotFound
		return
	}

	versionStr, ok := claims["version"].(string)
	if !ok || versionStr == "" {
		err = errVersionNotFound
		return
	}

	versionSplit := strings.Split(versionStr, ".")
	if len(versionSplit) != 2 {
		err = errVersionInvalid
		return
	}

	versionFlags, err := strconv.Atoi(versionSplit[1])
	if err != nil {
		return
	}

	turnRequired = (versionFlags & versionFlagMandatoryTurn) != 0
	subscriberSendAudio = (versionFlags & versionFlagAudioRequiredForReceive) != 0

	return
}

func createPeerConnection(url, bearerToken string, turnRequired bool, configureCallback func(peerConnection *webrtc.PeerConnection) error) (*webrtc.PeerConnection, error) {
	var (
		iceServers         []webrtc.ICEServer
		iceTransportPolicy = webrtc.ICETransportPolicyAll
		err                error
	)

	if turnRequired {
		iceTransportPolicy = webrtc.ICETransportPolicyRelay
		if iceServers, url, err = getIceCredentials(url, bearerToken); err != nil {
			return nil, err
		}

	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}

	s := webrtc.SettingEngine{}
	s.SetSRTPProtectionProfiles(dtls.SRTP_AES128_CM_HMAC_SHA1_80)
	s.SetRelayAcceptanceMinWait(time.Second)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(s))

	peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         iceServers,
		ICETransportPolicy: iceTransportPolicy,
	})
	if err != nil {
		return nil, err
	}

	if err = configureCallback(peerConnection); err != nil {
		return nil, err
	}
	readyToOffer, readyToOfferCancel := context.WithCancel(context.Background())

	if turnRequired {
		var once sync.Once
		peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
			if c == nil || c.Typ != webrtc.ICECandidateTypeRelay {
				return
			}

			once.Do(func() {
				readyToOfferCancel()
			})
		})
	} else {
		readyToOfferCancel()

	}

	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		return nil, err
	}

	if err := peerConnection.SetLocalDescription(offer); err != nil {
		return nil, err
	}

	<-readyToOffer.Done()
	if err := postOffer(bearerToken, url, peerConnection); err != nil {
		return nil, err
	}

	return peerConnection, nil
}

func postOffer(bearerToken, mediaServerURL string, peerConnection *webrtc.PeerConnection) error {
	req, err := http.NewRequest("POST", mediaServerURL, bytes.NewBuffer([]byte(peerConnection.LocalDescription().SDP)))
	if err != nil {
		return err
	}

	addToken(req, bearerToken)
	req.Header.Add("Content-Type", "application/sdp")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			addToken(req, bearerToken)
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("POST failed with error: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return peerConnection.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(body)})
}

// Send OPTIONs request to get ICE Credentials for a Contributor ID as a publish
func getIceCredentials(url, bearerToken string) (iceCredentials []webrtc.ICEServer, mediaServerURL string, err error) {
	optionsReq, err := http.NewRequest("OPTIONS", url, nil)
	if err != nil {
		return
	}

	addToken(optionsReq, bearerToken)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			addToken(req, bearerToken)
			return nil
		},
	}

	optionsResp, err := client.Do(optionsReq)
	if err != nil {
		return
	}
	defer optionsResp.Body.Close()

	mediaServerURL = optionsResp.Request.URL.String()

	if optionsResp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("OPTIONS failed with error: %s", optionsResp.Status)
	}

	for _, iceServer := range strings.Split(optionsResp.Header.Get("Link"), ",") {
		iceCredential := webrtc.ICEServer{}

		for _, val := range strings.Split(iceServer, ";") {
			if strings.HasPrefix(val, "<turn:") {
				iceCredential.URLs = []string{strings.TrimPrefix(strings.TrimSuffix(val, ">"), "<")}
			} else if split := strings.SplitN(val, "=", 2); len(split) == 2 {
				value := strings.TrimSpace(split[1])
				value = value[1 : len(value)-1]

				switch strings.TrimSpace(split[0]) {
				case "credential":
					iceCredential.Credential = value
				case "username":
					iceCredential.Username = value
				}
			}
		}
		iceCredentials = append(iceCredentials, iceCredential)
	}

	return
}

func addToken(req *http.Request, bearerToken string) {
	req.Header.Add("Authorization", "Bearer "+bearerToken)
}

func sendSilentAudio(audioTrack *webrtc.TrackLocalStaticSample) {
	audioDuration := 20 * time.Millisecond
	for ; true; <-time.NewTicker(audioDuration).C {
		_ = audioTrack.WriteSample(media.Sample{Data: []byte{0xFF, 0xFF, 0xFE}, Duration: audioDuration})
	}
}
