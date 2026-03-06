package clearnode

import (
	"context"
	"fmt"
	"strings"

	"github.com/erc7824/nitrolite/pkg/core"
	"github.com/erc7824/nitrolite/pkg/sign"
	sdk "github.com/erc7824/nitrolite/sdk/go"
	"github.com/shopspring/decimal"

	"faucet-server/internal/logger"
)

// TransferResult holds the result of a token transfer.
type TransferResult struct {
	TxID   string
	Amount string
	Asset  string
}

// Client wraps the Nitrolite SDK client for faucet operations.
type Client struct {
	sdkClient        *sdk.Client
	privateKeyHex    string
	clearnodeURL     string
	tokenSymbol      string
	tipAmount        decimal.Decimal
	minTransferCount int
}

func NewClient(privateKeyHex, clearnodeURL, tokenSymbol string, tipAmount decimal.Decimal, minTransferCount int) (*Client, error) {
	sdkClient, err := createSDKClient(privateKeyHex, clearnodeURL)
	if err != nil {
		return nil, err
	}

	return &Client{
		sdkClient:        sdkClient,
		privateKeyHex:    privateKeyHex,
		clearnodeURL:     clearnodeURL,
		tokenSymbol:      tokenSymbol,
		tipAmount:        tipAmount,
		minTransferCount: minTransferCount,
	}, nil
}

func createSDKClient(privateKeyHex, clearnodeURL string) (*sdk.Client, error) {
	msgSigner, err := sign.NewEthereumMsgSigner(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to create message signer: %w", err)
	}

	stateSigner, err := core.NewChannelDefaultSigner(msgSigner)
	if err != nil {
		return nil, fmt.Errorf("failed to create state signer: %w", err)
	}

	txSigner, err := sign.NewEthereumRawSigner(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to create tx signer: %w", err)
	}

	sdkClient, err := sdk.NewClient(clearnodeURL, stateSigner, txSigner)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Clearnode: %w", err)
	}

	return sdkClient, nil
}

// GetOwnerAddress returns the faucet owner's Ethereum address.
func (c *Client) GetOwnerAddress() string {
	return c.sdkClient.GetUserAddress()
}

// EnsureConnected checks the connection and reconnects if necessary.
func (c *Client) EnsureConnected() error {
	select {
	case <-c.sdkClient.WaitCh():
		logger.Info("Connection lost, reconnecting to Clearnode...")
		newClient, err := createSDKClient(c.privateKeyHex, c.clearnodeURL)
		if err != nil {
			return fmt.Errorf("failed to reconnect: %w", err)
		}
		c.sdkClient = newClient
		logger.Info("Successfully reconnected to Clearnode")
	default:
		// Connection is active
	}
	return nil
}

// EnsureOperational validates token support and sufficient balance.
func (c *Client) EnsureOperational() error {
	if err := c.validateTokenSupport(c.tokenSymbol); err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}

	if err := c.validateFaucetBalance(c.tokenSymbol, c.tipAmount, c.minTransferCount); err != nil {
		return fmt.Errorf("balance check failed: %w", err)
	}

	return nil
}

func (c *Client) validateTokenSupport(tokenSymbol string) error {
	ctx := context.Background()

	assets, err := c.sdkClient.GetAssets(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch supported assets: %w", err)
	}

	for _, asset := range assets {
		if strings.EqualFold(asset.Symbol, tokenSymbol) {
			logger.Debugf("Token '%s' is supported by Clearnode", tokenSymbol)
			return nil
		}
	}

	return fmt.Errorf("token '%s' is not supported by Clearnode", tokenSymbol)
}

func (c *Client) validateFaucetBalance(tokenSymbol string, tipAmount decimal.Decimal, minTransferCount int) error {
	ctx := context.Background()
	ownerAddress := c.sdkClient.GetUserAddress()

	balances, err := c.sdkClient.GetBalances(ctx, ownerAddress)
	if err != nil {
		return fmt.Errorf("failed to fetch faucet balance: %w", err)
	}

	minRequired := tipAmount.Mul(decimal.NewFromInt(int64(minTransferCount)))

	for _, balance := range balances {
		if strings.EqualFold(balance.Asset, tokenSymbol) {
			if balance.Balance.LessThan(minRequired) {
				return fmt.Errorf("insufficient %s balance: %s (required: %s for %d transfers)",
					tokenSymbol, balance.Balance.String(), minRequired.String(), minTransferCount)
			}
			logger.Infof("✓ Sufficient %s balance: %s", tokenSymbol, balance.Balance.String())
			return nil
		}
	}

	return fmt.Errorf("insufficient %s balance: 0 (required: %s for %d transfers)",
		tokenSymbol, minRequired.String(), minTransferCount)
}

// Transfer sends tokens to the destination address.
func (c *Client) Transfer(destination, asset string, amount decimal.Decimal) (*TransferResult, error) {
	ctx := context.Background()

	state, err := c.sdkClient.Transfer(ctx, destination, asset, amount)
	if err != nil {
		return nil, fmt.Errorf("transfer failed: %w", err)
	}

	result := &TransferResult{
		TxID:   state.Transition.TxID,
		Amount: state.Transition.Amount.String(),
		Asset:  state.Asset,
	}

	if result.Amount == "" || result.Amount == "0" {
		result.Amount = amount.String()
	}
	if result.Asset == "" {
		result.Asset = asset
	}

	return result, nil
}

// Close shuts down the Clearnode connection.
func (c *Client) Close() error {
	return c.sdkClient.Close()
}
