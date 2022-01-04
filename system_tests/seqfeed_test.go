//
// Copyright 2021, Offchain Labs, Inc. All rights reserved.
//

package arbtest

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/offchainlabs/arbstate/arbnode"
	"github.com/offchainlabs/arbstate/broadcastclient"
	"github.com/offchainlabs/arbstate/wsbroadcastserver"
)

var broadcasterConfigTest = wsbroadcastserver.BroadcasterConfig{
	Addr:          "127.0.0.1",
	IOTimeout:     5 * time.Second,
	Port:          "9642",
	Ping:          5 * time.Second,
	ClientTimeout: 15 * time.Second,
	Queue:         100,
	Workers:       100,
}

var broadcastClientConfigTest = broadcastclient.BroadcastClientConfig{
	URL:     "ws://localhost:9642/feed",
	Timeout: 20 * time.Second,
}

func TestSequencerFeed(t *testing.T) {
	/*
		glogger, _ := log.Root().GetHandler().(*log.GlogHandler)
		glogger.Verbosity(log.LvlTrace)
		defer func() { glogger.Verbosity(log.LvlInfo) }()
	*/

	seqNodeConfig := arbnode.NodeConfigL2Test
	seqNodeConfig.BatchPoster = true
	seqNodeConfig.Broadcaster = true
	seqNodeConfig.BroadcasterConfig = broadcasterConfigTest

	clientNodeConfig := arbnode.NodeConfigL2Test
	clientNodeConfig.BroadcastClient = true
	clientNodeConfig.BroadcastClientConfig = broadcastClientConfigTest

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l2info1, _ := CreateTestL2WithConfig(t, ctx, &seqNodeConfig)
	l2info2, _ := CreateTestL2WithConfig(t, ctx, &clientNodeConfig)

	client1 := l2info1.Client
	l2info1.GenerateAccount("User2")

	tx := l2info1.PrepareTx("Owner", "User2", 30000, big.NewInt(1e12), nil)

	err := client1.SendTransaction(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = arbnode.EnsureTxSucceeded(ctx, client1, tx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = arbnode.WaitForTx(ctx, l2info2.Client, tx.Hash(), time.Second*15)
	if err != nil {
		t.Fatal(err)
	}
	l2balance, err := l2info2.Client.BalanceAt(ctx, l2info1.GetAddress("User2"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l2balance.Cmp(big.NewInt(1e12)) != 0 {
		t.Fatal("Unexpected balance:", l2balance)
	}
}

func TestLyingSequencer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The truthful sequencer
	nodeConfigA := arbnode.NodeConfigL1Test
	nodeConfigA.BatchPoster = true
	nodeConfigA.Broadcaster = false
	l2infoA, nodeA, _, _, l1stack := CreateTestNodeOnL1WithConfig(t, ctx, true, &nodeConfigA)
	defer l1stack.Close()
	l2clientA := l2infoA.Client

	// The lying sequencer
	nodeConfigC := arbnode.NodeConfigL1Test
	nodeConfigC.BatchPoster = false
	nodeConfigC.Broadcaster = true
	nodeConfigC.BroadcasterConfig = broadcasterConfigTest
	l2clientC, _ := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &nodeConfigC)

	// The client node, connects to lying sequencer's feed
	nodeConfigB := arbnode.NodeConfigL1Test
	nodeConfigB.Broadcaster = false
	nodeConfigB.BatchPoster = false
	nodeConfigB.BroadcastClient = true
	nodeConfigB.BroadcastClientConfig = broadcastClientConfigTest
	l2clientB, _ := Create2ndNodeWithConfig(t, ctx, nodeA, l1stack, &nodeConfigB)

	l2infoA.GenerateAccount("FraudUser")
	l2infoA.GenerateAccount("RealUser")

	fraudTx := l2infoA.PrepareTx("Owner", "FraudUser", 30000, big.NewInt(1e12), nil)
	l2infoA.GetInfoWithPrivKey("Owner").Nonce -= 1 // Use same l2info object for different l2s
	realTx := l2infoA.PrepareTx("Owner", "RealUser", 30000, big.NewInt(1e12), nil)

	err := l2clientC.SendTransaction(ctx, fraudTx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = arbnode.EnsureTxSucceeded(ctx, l2clientC, fraudTx)
	if err != nil {
		t.Fatal(err)
	}

	// Node B should get the transaction immediately from the sequencer feed
	_, err = arbnode.WaitForTx(ctx, l2clientB, fraudTx.Hash(), time.Second*5)
	if err != nil {
		t.Fatal(err)
	}
	l2balance, err := l2clientB.BalanceAt(ctx, l2infoA.GetAddress("FraudUser"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l2balance.Cmp(big.NewInt(1e12)) != 0 {
		t.Fatal("Unexpected balance:", l2balance)
	}

	// Send the real transaction to client A
	err = l2clientA.SendTransaction(ctx, realTx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = arbnode.EnsureTxSucceeded(ctx, l2clientA, realTx)
	if err != nil {
		t.Fatal(err)
	}

	// Node B should get the transaction after NodeC posts a batch.
	_, err = arbnode.WaitForTx(ctx, l2clientB, realTx.Hash(), time.Second*5)
	if err != nil {
		t.Fatal(err)
	}
	l2balanceFraudAcct, err := l2clientB.BalanceAt(ctx, l2infoA.GetAddress("FraudUser"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l2balanceFraudAcct.Cmp(big.NewInt(0)) != 0 {
		t.Fatal("Unexpected balance (fraud acct should be empty) was:", l2balanceFraudAcct)
	}

	l2balanceRealAcct, err := l2clientB.BalanceAt(ctx, l2infoA.GetAddress("RealUser"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if l2balanceRealAcct.Cmp(big.NewInt(1e12)) != 0 {
		t.Fatal("Unexpected balance:", l2balanceRealAcct)
	}
}
