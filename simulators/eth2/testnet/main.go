package main

import (
	"fmt"
	"github.com/ethereum/hive/hivesim"
	"github.com/ethereum/hive/internal/libhive"
	"github.com/ethereum/hive/simulators/eth2/testnet/setup"
	"github.com/protolambda/zrnt/eth2/configs"
	"strings"
	"time"
)

func main() {
	var suite = hivesim.Suite{
		Name:        "eth2-testnet",
		Description: `Run different eth2 testnets.`,
	}
	suite.Add(hivesim.TestSpec{
		Name:        "testnets",
		Description: "Collection of different testnet compositions and assertions.",
		Run: func(t *hivesim.T) {
			clientTypes, err := t.Sim.ClientTypes()
			if err != nil {
				t.Fatal(err)
			}
			nodeChoices(clientTypes).runTests(t)
		},
	})
	hivesim.MustRunSuite(hivesim.New(), suite)
}

type NodeChoices struct {
	beacon    []string
	validator []string
	eth1      []string
	other     []string
}

func nodeChoices(available []*libhive.ClientDefinition) *NodeChoices {
	var out NodeChoices
	for _, client := range available {
		switch client.Meta.Role {
		case "beacon":
			out.beacon = append(out.beacon, client.Name)
		case "validator":
			out.validator = append(out.validator, client.Name)
		case "eth1":
			out.eth1 = append(out.eth1, client.Name)
		default:
			out.other = append(out.other, client.Name)
		}
	}
	return &out
}

func ComposeTestnetTest(name string, description string, build ...TestnetDecorator) hivesim.TestSpec {
	return hivesim.TestSpec{
		Name:        name,
		Description: description,
		Run: func(t *hivesim.T) {
			testnet := NewTestnet(t)
			for _, td := range build {
				td(testnet)
			}
			// TODO: change this based on testnet configuration, or do polling
			time.Sleep(time.Minute * 10)

			// TODO: ask all beacon nodes if they have finalized
			for _, b := range testnet.beacons {
				// apiAddr := fmt.Sprintf("http://%s:4000", b.IP)
				testnet.t.Errorf("node %s failed to finalize", b.Container)
				// TODO use Go API bindings to request finality status, error per non-finalized node.
			}
		},
	}
}

func (nc *NodeChoices) runTests(t *hivesim.T) {

	for _, beaconType := range nc.beacon {
		// only generate data once
		prep := prepareTestnet(t)

		validatorType, err := nc.preferredValidator(beaconType)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(ComposeTestnetTest(
			"single-client-testnet",
			"This runs quick eth2 single-client testnets, beacon nodes matched with preferred validator type, and dummy eth1 endpoint.",
			func(testnet *Testnet) {
				for i := 0; i < len(prep.keys); i++ {
					prep.buildBeaconNode(beaconType, nil)(testnet)
					prep.buildValidatorClient(validatorType, i, i)(testnet)
				}
			},
		))
	}

	/*
		TODO More testnet ideas:

		Name:        "two-client-testnet",
		Description: "This runs quick eth2 testnets with combinations of 2 client types, beacon nodes matched with preferred validator type, and dummy eth1 endpoint.",
		Name:        "all-client-testnet",
		Description: "This runs a quick eth2 testnet with all client types, beacon nodes matched with preferred validator type, and dummy eth1 endpoint.",
		Name:        "cross-single-client-testnet",
		Description: "This runs a quick eth2 single-client testnet, but beacon nodes are matched with all validator types",
	*/
}

func (nc *NodeChoices) preferredValidator(beacon string) (string, error) {
	beaconVendor := strings.Split(beacon, "-")[0]
	for _, v := range nc.validator {
		valVendor := strings.Split(v, "-")[0]
		if beaconVendor == valVendor {
			return v, nil
		}
	}
	return "", fmt.Errorf("no preferred validator for beacon %s", beacon)
}

// PreparedTestnet has all the options for starting nodes, ready to build the network.
type PreparedTestnet struct {
	commonValidatorParams hivesim.StartOption
	commonBeaconParams    hivesim.StartOption
	configOpt             hivesim.StartOption
	stateOpt              hivesim.StartOption
	keys                  []hivesim.StartOption
}

func prepareTestnet(t *hivesim.T) *PreparedTestnet {
	// TODO: copy and modify with env vars
	spec := configs.Mainnet // TODO: mod config
	valCount := uint64(4096)
	keyPartitions := uint64(4)

	eth2Config := hivesim.WithParams(map[string]string{
		"HIVE_ETH2_FOOBAR": "1234",
		// TODO: load mainnet config, but custom vars
	})

	mnemonic := "couple kiwi radio river setup fortune hunt grief buddy forward perfect empty slim wear bounce drift execute nation tobacco dutch chapter festival ice fog"
	keySrc := &setup.MnemonicsKeySource{
		From:       0,
		To:         valCount,
		Validator:  mnemonic,
		Withdrawal: mnemonic,
	}
	keys, err := keySrc.Keys()
	if err != nil {
		t.Fatal(err)
	}
	keyOpts := make([]hivesim.StartOption, keyPartitions)
	for i := uint64(0); i < keyPartitions; i++ {
		// Give each validator client an equal subset of the genesis validator keys
		startIndex := valCount * i / keyPartitions
		endIndex := valCount * (i + 1) / keyPartitions
		keysOpt, err := setup.KeysTar(keys[startIndex:endIndex])
		if err != nil {
			t.Fatal(err)
		}
		keyOpts = append(keyOpts, keysOpt)
	}

	// prepare genesis beacon state, with all of the validators in it.
	state, err := setup.BuildPhase0State(spec, keys)
	if err != nil {
		t.Fatal(err)
	}

	beaconParams := hivesim.WithParams(hivesim.Params{
		"HIVE_ETH2_BN_API_PORT": "4000",
	})
	validatorParams := hivesim.WithParams(hivesim.Params{
		"HIVE_ETH2_BN_API_PORT": "4000",
	})

	// we need a new genesis time, so we take the template state and prepare a tar with updated time
	stateOpt, err := setup.StateTar(state, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	return &PreparedTestnet{
		commonBeaconParams:    beaconParams,
		commonValidatorParams: validatorParams,
		configOpt:             eth2Config,
		stateOpt:              stateOpt,
		keys:                  keyOpts,
	}
}

type TestnetDecorator func(testnet *Testnet)

type Testnet struct {
	t          *hivesim.T
	beacons    []*hivesim.Client
	validators []*hivesim.Client
	eth1       []*hivesim.Client
}

func NewTestnet(t *hivesim.T) *Testnet {
	return &Testnet{t: t}
}

func (p *PreparedTestnet) buildEth1Node(eth1Type string) func(testnet *Testnet) {
	return func(testnet *Testnet) {
		// TODO: eth1 node options: custom eth1 chain to back deposits for eth2 testnet...
		n := testnet.t.StartClientWithOptions(eth1Type)
		testnet.eth1 = append(testnet.eth1, n)
	}
}

func (p *PreparedTestnet) buildBeaconNode(beaconType string, eth1Endpoints []int) TestnetDecorator {
	return func(testnet *Testnet) {
		// Hook up validator to beacon node
		params := hivesim.Params{}
		if len(eth1Endpoints) > 0 {
			var addrs []string
			for _, index := range eth1Endpoints {
				if index >= len(testnet.eth1) {
					testnet.t.Fatalf("only have %d eth1 nodes, cannot find index %d for BN", len(testnet.eth1), index)
				}
				eth1Node := testnet.eth1[index]
				addrs = append(addrs, fmt.Sprintf("http://%v:8545", eth1Node.IP))
			}
			params["ETH1_RPC_ADDRS"] = strings.Join(addrs, ",")
		}
		bn := testnet.t.StartClientWithOptions(
			beaconType, p.configOpt, p.stateOpt, p.commonBeaconParams, hivesim.WithParams(params))
		testnet.beacons = append(testnet.beacons, bn)
	}
}

func (p *PreparedTestnet) buildValidatorClient(validatorType string, bnIndex int, keyIndex int) func(testnet *Testnet) {
	return func(testnet *Testnet) {
		if bnIndex >= len(testnet.beacons) {
			testnet.t.Fatalf("only have %d beacon nodes, cannot find index %d for VC", len(testnet.beacons), bnIndex)
		}
		bn := testnet.beacons[bnIndex]
		// Hook up validator to beacon node
		bnAPIOpt := hivesim.WithParams(hivesim.Params{
			"HIVE_ETH2_BN_API_IP": bn.IP.String(),
		})
		if keyIndex >= len(p.keys) {
			testnet.t.Fatalf("only have %d key tranches, cannot find index %d for VC", len(p.keys), keyIndex)
		}
		keysOpt := p.keys[keyIndex]
		vc := testnet.t.StartClientWithOptions(validatorType, p.configOpt, keysOpt, p.commonValidatorParams, bnAPIOpt)
		testnet.validators = append(testnet.validators, vc)
	}
}
