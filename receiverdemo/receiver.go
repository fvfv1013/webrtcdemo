package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
	"os"
)

func main() {
	resvBuf := make(chan *webrtc.DataChannelMessage, 100)

	// 1. PeerConnection
	api := webrtc.NewAPI()
	connection, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
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
	DataChOpen := make(chan struct{}, 10)
	var DataCh *webrtc.DataChannel
	_ = DataCh
	connection.OnDataChannel(func(ch *webrtc.DataChannel) {
		switch ch.Label() {
		case "data":
			DataCh = ch

			ch.OnOpen(func() {
				fmt.Println("dataCh Open")
				DataChOpen <- struct{}{}
			})

			ch.OnMessage(func(msg webrtc.DataChannelMessage) {
				resvBuf <- &msg
			})
		}
	})
	connection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		fmt.Println("Candidate found", candidate)
		if candidate == nil {
			return
		}
	})

	// 3. readOffer
	//reader := io.NewSectionReader(os.Stdin, 0, 200)
	fmt.Printf("Input Sender Offer:")
	reader := bufio.NewReader(os.Stdin)
	var ResOffer string
	_, err = fmt.Fscanf(reader, "%s", &ResOffer)

	if err := ValidateSDP(ResOffer); err != nil {
		log.Fatal("valid:", err)
	}
	RemoteSDP, err := DecodeSDP(ResOffer)
	if err != nil {
		log.Fatal("decode:", err)
	}
	//fmt.Println("RemoteSDP:", RemoteSDP)
	err = connection.SetRemoteDescription(*RemoteSDP)
	if err != nil {
		return
	}

	// 4. SendAnswer
	initAnswer, err := connection.CreateAnswer(nil)
	if err != nil {
		log.Fatal("init:", err)
	}
	err = connection.SetLocalDescription(initAnswer)
	if err != nil {
		log.Fatal("setlocal:", err)
	}
	fmt.Println("LocalSDP:", connection.LocalDescription())
	SendOffer, err := EncodeSDP(connection.LocalDescription())
	if err != nil {
		log.Fatal("encodeoffer:", err)
	}
	fmt.Println("SendOffer:", SendOffer)

	// 5. ReadData
	<-DataChOpen

	for {
		rmsg := <-resvBuf
		fmt.Println(rmsg.Data)
		//reader := io.LimitReader(os.Stdin, 100)
		//buf, err := io.ReadAll(reader)
		//if err != nil {
		//	log.Print(err)
		//}
		//err = dataChannel.Send(buf)
		//if err != nil {
		//	return
		//}
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

//func sendCandidatesHandler(Conn *webrtc.PeerConnection) {
//	Conn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
//		if candidate == nil {
//			return
//		}
//
//		candidateBytes, err := json.Marshal(candidate.ToJSON())
//		if err != nil {
//			slog.Error("Error marshalling candidate:", err)
//			return
//		}
//		err = candidateCh.Send(candidateBytes)
//		if err != nil {
//			slog.Error("Error sending candidate:", err)
//		}
//	})
//}
