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
	resvBuf := make(chan *webrtc.DataChannelMessage, 100)

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
	var CandidateChOpen atomic.Bool
	DataChOpen := make(chan struct{}, 10)
	var DataCh *webrtc.DataChannel
	_ = DataCh
	var CandidateCh *webrtc.DataChannel
	connection.OnDataChannel(func(ch *webrtc.DataChannel) {
		switch ch.Label() {
		case "candidate":
			CandidateCh = ch

			ch.OnOpen(func() {
				fmt.Println("candidateCH Open")
				CandidateChOpen.Store(true)
			})

			ch.OnMessage(func(msg webrtc.DataChannelMessage) {
				var candidate webrtc.ICECandidateInit
				err := json.Unmarshal(msg.Data, &candidate)
				if err != nil {
					slog.Error("Error unmarshalling candidate:", err)
					return
				}
				err = connection.AddICECandidate(candidate)
				if err != nil {
					slog.Error("Error adding ice candidate:", err)
				}
			})
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
		err = CandidateCh.Send(candidateBytes)
		if err != nil {
			slog.Error("Error sending candidate:", err)
		}
	})

	// 3. readOffer
	//reader := io.NewSectionReader(os.Stdin, 0, 200)
	fmt.Printf("Input Sender Offer:")
	reader := io.LimitReader(os.Stdin, 2000)
	var ResOffer string
	_, err = fmt.Fscanf(reader, "%s", &ResOffer)

	if err := ValidateSDP(string(ResOffer)); err != nil {
		log.Fatal("valid:", err)
	}
	RemoteSDP, err := DecodeSDP(string(ResOffer))
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
	CandidateConn.L.Lock()
	CandidateConn.Wait()
	CandidateConn.L.Unlock()
	//fmt.Println("LocalSDP:", connection.LocalDescription())
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
		return
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
