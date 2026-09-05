package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
)

type httpRemoteSetupServer struct {
	account *httpAccountServer
}

func newHTTPRemoteSetupServer(account *httpAccountServer) *httpRemoteSetupServer {
	return &httpRemoteSetupServer{account: account}
}

func (s *httpRemoteSetupServer) createChallenge(ctx context.Context, accessToken, hostID string) (remoteSetupChallenge, error) {
	var challenge remoteSetupChallenge
	err := s.account.request(ctx, http.MethodPost, "/v1/pairing/challenges", accessToken, map[string]string{"hostID": hostID}, &challenge)
	return challenge, err
}

func (s *httpRemoteSetupServer) challengeResult(ctx context.Context, accessToken, challengeID string) (remoteSetupResult, error) {
	var result remoteSetupResult
	err := s.account.request(ctx, http.MethodGet, "/v1/pairing/challenges/"+url.PathEscape(challengeID), accessToken, nil, &result)
	return result, err
}

func (s *httpRemoteSetupServer) cancelChallenge(ctx context.Context, accessToken, challengeID string) error {
	return s.account.request(ctx, http.MethodDelete, "/v1/pairing/challenges/"+url.PathEscape(challengeID), accessToken, nil, nil)
}

func (s *httpRemoteSetupServer) registerLANPairing(ctx context.Context, accessToken string, intent lanPairingIntent) (pairedDevice, error) {
	var paired pairedDevice
	err := s.account.request(ctx, http.MethodPost, "/v1/pairing/lan", accessToken, intent, &paired)
	return paired, err
}

func (s *httpRemoteSetupServer) stageLANPairing(ctx context.Context, accessToken string, intent lanPairingIntent) error {
	return s.account.request(ctx, http.MethodPost, "/v1/pairing/lan", accessToken, intent, nil)
}

func (s *httpRemoteSetupServer) confirmPairing(ctx context.Context, accessToken, hostID string, paired pairedDevice) error {
	return s.account.request(ctx, http.MethodPost, "/v1/pairings/confirm", accessToken, map[string]any{
		"pairingID": paired.PairingID, "hostID": hostID, "deviceID": paired.DeviceID,
		"deviceIdentityPublicKey": base64.StdEncoding.EncodeToString(paired.DeviceIdentityPublicKey),
	}, nil)
}
