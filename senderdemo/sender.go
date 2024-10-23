package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
)

func main() {

	CandidateConn := sync.Cond{L: &sync.Mutex{}}
	// 1. PeerConnection
	api := webrtc.NewAPI()
	connection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun.l.google.com:5349"}},
		},
	})
	if err != nil {
		log.Print(err)
		return
	}
	defer func(connection *webrtc.PeerConnection) {
		err := connection.GracefulClose()
		if err != nil {
			log.Print(err)
			return
		}
	}(connection)
	connection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		fmt.Println("ICEstate:", state)
	})

	// 2. DataChannel
	dataCh, err := connection.CreateDataChannel("data", &webrtc.DataChannelInit{})
	if err != nil {
		log.Print(err)
		return
	}
	defer func(dataChannel *webrtc.DataChannel) {
		err := dataChannel.GracefulClose()
		if err != nil {
			log.Print(err)
			return
		}
	}(dataCh)
	DataChOpen := make(chan struct{})
	dataCh.OnOpen(func() {
		fmt.Println("dataCh.Open")
		DataChOpen <- struct{}{}
	})
	//dataCh.OnMessage(func(msg webrtc.DataChannelMessage) {
	//	resvBuf <- msg
	//})
	candidateCh, err := connection.CreateDataChannel("candidate", &webrtc.DataChannelInit{})
	if err != nil {
		log.Print(err)
		return
	}
	defer func(dataChannel *webrtc.DataChannel) {
		err := dataChannel.GracefulClose()
		if err != nil {
			log.Print(err)
			return
		}
	}(candidateCh)
	var CandidateChOpen atomic.Bool
	candidateCh.OnOpen(func() {
		fmt.Println("candidateCh.Open")
		CandidateChOpen.Store(true)
	})
	connection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		fmt.Println("Candidate found", candidate)
		if candidate == nil {
			return
		}

		CandidateConn.L.Lock()
		CandidateConn.Broadcast()
		CandidateConn.L.Unlock()

		if !CandidateChOpen.Load() {
			return
		}

		candidateBytes, err := json.Marshal(candidate.ToJSON())
		if err != nil {
			slog.Error("Error marshalling candidate:", err)
			return
		}
		err = candidateCh.Send(candidateBytes)
		if err != nil {
			slog.Error("Error sending candidate:", err)
		}
	})

	// 3. SendOffer
	initOffer, err := connection.CreateOffer(nil)
	if err != nil {
		log.Print(err)
		return
	}
	err = connection.SetLocalDescription(initOffer)
	if err != nil {
		log.Print(err)
		return
	}
	CandidateConn.L.Lock()
	CandidateConn.Broadcast()
	CandidateConn.L.Unlock()
	//fmt.Println("LocalSDP:", connection.LocalDescription())
	SendOffer, err := EncodeSDP(connection.LocalDescription())
	if err != nil {
		log.Print(err)
		return
	}
	fmt.Println("SendOffer:", SendOffer)

	// 4. Receive Answer
	//reader := io.NewSectionReader(os.Stdin, 0, 200)
	fmt.Printf("Input Receiver Offer:")
	reader := io.LimitReader(os.Stdin, 2000)
	var ResOffer string
	_, err = fmt.Fscanf(reader, "%s", &ResOffer)
	if err != nil {
		return
	}
	if err := ValidateSDP(string(ResOffer)); err != nil {
		log.Print(err)
		return
	}
	RemoteSDP, err := DecodeSDP(string(ResOffer))
	if err != nil {
		log.Print(err)
		return
	}
	//fmt.Println("RemoteSDP:", RemoteSDP)
	err = connection.SetRemoteDescription(*RemoteSDP)
	if err != nil {
		return
	}

	// 5. SendData
	<-DataChOpen
	for {
		reader := io.LimitReader(os.Stdin, 100)
		//buf, err := io.ReadAll(reader)
		var buf string
		_, _ = fmt.Fscanf(reader, "%s", &buf)
		//if err != nil {
		//	log.Print(err)
		return
		//}
		err = dataCh.Send([]byte(buf))
		if err != nil {
			return
		}
		fmt.Println(buf, "has been sent")
	}
}

func EncodeSDP(sdp *webrtc.SessionDescription) (string, error) {
	sdpJSON, err := json.Marshal(sdp)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	g, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	defer g.Close()
	if _, err = g.Write(sdpJSON); err != nil {
		return "", err
	}

	if err = g.Close(); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func DecodeSDP(in string) (*webrtc.SessionDescription, error) {
	buf, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	sdpBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var sdp webrtc.SessionDescription
	err = json.Unmarshal(sdpBytes, &sdp)
	if err != nil {
		return nil, err
	}

	return &sdp, nil
}

func ValidateSDP(input string) error {
	buf, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return err
	}
	r, err := gzip.NewReader(bytes.NewReader(buf))
	if err != nil {
		return err
	}
	defer r.Close()

	sdpBytes, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	var sdp webrtc.SessionDescription
	err = json.Unmarshal(sdpBytes, &sdp)
	if err != nil {
		return err
	}

	return nil
}
