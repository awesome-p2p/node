package client_connection

import (
	"errors"
	"github.com/mysterium/node/communication"
	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/openvpn"
	"github.com/mysterium/node/openvpn/middlewares/client/bytescount"
	"github.com/mysterium/node/openvpn/middlewares/client/state"
	"github.com/mysterium/node/server"
	dto_discovery "github.com/mysterium/node/service_discovery/dto"
	"github.com/mysterium/node/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
)

type testContext struct {
	suite.Suite
	connManager         *connectionManager
	fakeDiscoveryClient *server.ClientFake
	fakeOpenVpn         *fakeOpenvpnClient
	fakeStatsKeeper     *fakeSessionStatsKeeper
}

var (
	activeProviderID      = "vpn-node-1"
	activeProviderContact = dto_discovery.Contact{}
	activeProposal        = dto_discovery.ServiceProposal{
		ProviderID:       activeProviderID,
		ProviderContacts: []dto_discovery.Contact{activeProviderContact},
	}
)

func (tc *testContext) SetupTest() {
	tc.fakeDiscoveryClient = server.NewClientFake()
	tc.fakeDiscoveryClient.RegisterProposal(activeProposal, nil)

	dialogEstablisherFactory := func(identity identity.Identity) communication.DialogEstablisher {
		return &fakeDialog{}
	}

	tc.fakeOpenVpn = &fakeOpenvpnClient{
		nil,
		nil,
	}
	fakeVpnClientFactory := func(vpnSession session.SessionDto, identity identity.Identity, callback state.ClientStateCallback) (openvpn.Client, error) {
		tc.fakeOpenVpn.stateCallback = callback
		return tc.fakeOpenVpn, nil
	}
	tc.fakeStatsKeeper = &fakeSessionStatsKeeper{}

	tc.connManager = NewManager(tc.fakeDiscoveryClient, dialogEstablisherFactory, fakeVpnClientFactory, tc.fakeStatsKeeper)
}

func (tc *testContext) TestWhenNoConnectionIsMadeStatusIsNotConnected() {
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())
}

func (tc *testContext) TestWithUnknownNodeKeyConnectionIsNotMade() {
	noProposalsError := errors.New("node has no service proposals")

	assert.Equal(tc.T(), noProposalsError, tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress("unknown-node")))
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())

	assert.False(tc.T(), tc.fakeStatsKeeper.sessionStartMarked)
}

func (tc *testContext) TestOnConnectErrorStatusIsNotConnectedAndLastErrorIsSetAndSessionStartIsNotMarked() {
	fatalVpnError := errors.New("fatal connection error")
	tc.fakeOpenVpn.onConnectReturnError = fatalVpnError

	assert.Error(tc.T(), tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress(activeProviderID)))
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())

	assert.False(tc.T(), tc.fakeStatsKeeper.sessionStartMarked)
}

func (tc *testContext) TestWhenManagerMadeConnectionStatusReturnsConnectedStateAndSessionId() {
	err := tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress(activeProviderID))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	assert.NoError(tc.T(), err)
	assert.Equal(tc.T(), ConnectionStatus{Connected, "vpn-connection-id", nil}, tc.connManager.Status())
}

func (tc *testContext) TestWhenManagerMadeConnectionSessionStartIsMarked() {
	err := tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress("vpn-node-1"))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	assert.NoError(tc.T(), err)

	assert.True(tc.T(), tc.fakeStatsKeeper.sessionStartMarked)
}

func (tc *testContext) TestStatusReportsConnectingWhenConnectionIsInProgress() {
	tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress(activeProviderID))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	assert.Equal(tc.T(), ConnectionStatus{Connecting, "", nil}, tc.connManager.Status())
}

func (tc *testContext) TestStatusReportsDisconnectingThenNotConnected() {
	err := tc.connManager.Connect(identity.FromAddress("identity-1"), identity.FromAddress(activeProviderID))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	assert.NoError(tc.T(), err)
	assert.Equal(tc.T(), ConnectionStatus{Connected, "vpn-connection-id", nil}, tc.connManager.Status())

	assert.NoError(tc.T(), tc.connManager.Disconnect())
	assert.Equal(tc.T(), ConnectionStatus{Disconnecting, "", nil}, tc.connManager.Status())

	tc.fakeOpenVpn.reportState(openvpn.STATE_EXITING)
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())
}

func (tc *testContext) TestConnectResultsInAlreadyConnectedErrorWhenConnectionExists() {
	assert.NoError(tc.T(), tc.connManager.Connect(identity.FromAddress("my-identtiy"), identity.FromAddress(activeProviderID)))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	assert.Equal(tc.T(), AlreadyExists, tc.connManager.Connect(identity.FromAddress("my-identity"), identity.FromAddress(activeProviderID)))
}

func (tc *testContext) TestDisconnectReturnsErrorWhenNoConnectionExists() {
	assert.Equal(tc.T(), NoConnection, tc.connManager.Disconnect())
}

func (tc *testContext) TestReconnectingStatusIsReportedWhenOpenVpnGoesIntoReconnectingState() {
	assert.NoError(tc.T(), tc.connManager.Connect(identity.FromAddress("my-identtiy"), identity.FromAddress(activeProviderID)))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	tc.fakeOpenVpn.reportState(openvpn.STATE_RECONNECTING)
	assert.Equal(tc.T(), ConnectionStatus{Reconnecting, "", nil}, tc.connManager.Status())
}

func (tc *testContext) TestNotConnectedStatusIsReportedWhenOpenvpnReportsExiting() {
	assert.NoError(tc.T(), tc.connManager.Connect(identity.FromAddress("my-identtiy"), identity.FromAddress(activeProviderID)))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)
	tc.fakeOpenVpn.reportState(openvpn.STATE_EXITING)
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())
}

func (tc *testContext) TestDoubleDisconnectResultsInError() {
	assert.NoError(tc.T(), tc.connManager.Connect(identity.FromAddress("my-identtiy"), identity.FromAddress(activeProviderID)))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)

	assert.NoError(tc.T(), tc.connManager.Disconnect())
	tc.fakeOpenVpn.reportState(openvpn.STATE_EXITING)
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())
	assert.Equal(tc.T(), NoConnection, tc.connManager.Disconnect())
}

func (tc *testContext) TestTwoConnectDisconnectCyclesReturnNoError() {
	assert.NoError(tc.T(), tc.connManager.Connect(identity.FromAddress("my-identtiy"), identity.FromAddress(activeProviderID)))
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTING)
	tc.fakeOpenVpn.reportState(openvpn.STATE_CONNECTED)

	assert.NoError(tc.T(), tc.connManager.Disconnect())
	tc.fakeOpenVpn.reportState(openvpn.STATE_EXITING)
	assert.Equal(tc.T(), ConnectionStatus{NotConnected, "", nil}, tc.connManager.Status())
}

func TestConnectionManagerSuite(t *testing.T) {
	suite.Run(t, new(testContext))
}

type fakeOpenvpnClient struct {
	onConnectReturnError error
	stateCallback        state.ClientStateCallback
}

func (foc *fakeOpenvpnClient) Start() error {
	return foc.onConnectReturnError
}

func (foc *fakeOpenvpnClient) Wait() error {
	return nil
}

func (foc *fakeOpenvpnClient) Stop() error {
	return nil
}

func (foc *fakeOpenvpnClient) reportState(state openvpn.State) {
	foc.stateCallback(state)
	//this is a hacky way to give goroutines in connection manager a chance to consume incoming events form channels
	time.Sleep(time.Millisecond)
}

type fakeDialog struct {
	peerId identity.Identity
}

func (fd *fakeDialog) CreateDialog(peerID identity.Identity, peerContact dto_discovery.Contact) (communication.Dialog, error) {
	fd.peerId = peerID
	return fd, nil
}

func (fd *fakeDialog) PeerID() identity.Identity {
	return fd.peerId
}

func (fd *fakeDialog) Close() error {
	return nil
}

func (fd *fakeDialog) Receive(consumer communication.MessageConsumer) error {
	return nil
}
func (fd *fakeDialog) Respond(consumer communication.RequestConsumer) error {
	return nil
}

func (fd *fakeDialog) Send(producer communication.MessageProducer) error {
	return nil
}

func (fd *fakeDialog) Request(producer communication.RequestProducer) (responsePtr interface{}, err error) {
	return &session.SessionCreateResponse{
			Success: true,
			Message: "Everything is great!",
			Session: session.SessionDto{
				ID:     "vpn-connection-id",
				Config: "vpn-connection-config",
			},
		},
		nil
}

type fakeSessionStatsKeeper struct {
	sessionStartMarked bool
}

func (fsk *fakeSessionStatsKeeper) Save(stats bytescount.SessionStats) {
}

func (fsk *fakeSessionStatsKeeper) Retrieve() bytescount.SessionStats {
	return bytescount.SessionStats{}
}

func (fsk *fakeSessionStatsKeeper) MarkSessionStart() {
	fsk.sessionStartMarked = true
}

func (fsk *fakeSessionStatsKeeper) GetSessionDuration() time.Duration {
	return time.Duration(0)
}
