package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbent "github.com/yx-token/token-market/ent"
	"github.com/yx-token/token-market/internal/payment"
	infraerrors "github.com/yx-token/token-market/internal/pkg/errors"
	"github.com/yx-token/token-market/market/hostpay"
)

func TestProviderMutationsRespectMarketInFlightOrders(t *testing.T) {
	tests := []struct {
		name      string
		marketCnt int
		mutate    func(*PaymentConfigService, int64) error
	}{
		{
			name:      "delete",
			marketCnt: 2,
			mutate: func(svc *PaymentConfigService, id int64) error {
				return svc.DeleteProviderInstance(context.Background(), id)
			},
		},
		{
			name:      "disable",
			marketCnt: 1,
			mutate: func(svc *PaymentConfigService, id int64) error {
				_, err := svc.UpdateProviderInstance(context.Background(), id, UpdateProviderInstanceRequest{Enabled: hostpayProtectionBool(false)})
				return err
			},
		},
		{
			name:      "replace protected credentials",
			marketCnt: 3,
			mutate: func(svc *PaymentConfigService, id int64) error {
				_, err := svc.UpdateProviderInstance(context.Background(), id, UpdateProviderInstanceRequest{
					Config: map[string]string{"privateKey": "replacement-key"},
				})
				return err
			},
		},
	}

	for _, providerKey := range []string{payment.TypeAlipay, payment.TypeWxpay} {
		for _, tt := range tests {
			t.Run(providerKey+"/"+tt.name, func(t *testing.T) {
				client := newPaymentConfigServiceTestClient(t)
				svc := newHostpayProtectionTestService(client)
				instance := createHostpayProtectionTestInstance(t, svc, providerKey, true)
				calls := 0
				hostpay.SetInFlightCounter(func(_ context.Context, id int64) (int, error) {
					calls++
					require.Equal(t, instance.ID, id)
					return tt.marketCnt, nil
				})
				t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })

				err := tt.mutate(svc, instance.ID)
				require.Error(t, err)
				require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
				require.Equal(t, 1, calls)
				appErr := infraerrors.FromError(err)
				require.Equal(t, "0", appErr.Metadata["sub2api_count"])
				wantCount := strconv.Itoa(tt.marketCnt)
				require.Equal(t, wantCount, appErr.Metadata["market_count"])
				require.Equal(t, wantCount, appErr.Metadata["count"])
				require.Contains(t, appErr.Message, "闲置市场还有 "+wantCount+" 笔在途充值/退款")
				saved, loadErr := client.PaymentProviderInstance.Get(context.Background(), instance.ID)
				require.NoError(t, loadErr)
				require.Equal(t, instance.Config, saved.Config)
				require.True(t, saved.Enabled)
			})
		}
	}
}

func TestProviderMutationsAllowWhenMarketHasNoInFlightOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := newHostpayProtectionTestService(client)
	var calls []int64
	hostpay.SetInFlightCounter(func(_ context.Context, id int64) (int, error) {
		calls = append(calls, id)
		return 0, nil
	})
	t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })

	toDelete := createHostpayProtectionTestInstance(t, svc, payment.TypeWxpay, false)
	require.NoError(t, svc.DeleteProviderInstance(ctx, toDelete.ID))
	toDisable := createHostpayProtectionTestInstance(t, svc, payment.TypeAlipay, true)
	_, err := svc.UpdateProviderInstance(ctx, toDisable.ID, UpdateProviderInstanceRequest{Enabled: hostpayProtectionBool(false)})
	require.NoError(t, err)
	toRotate := createHostpayProtectionTestInstance(t, svc, payment.TypeAlipay, true)
	_, err = svc.UpdateProviderInstance(ctx, toRotate.ID, UpdateProviderInstanceRequest{
		Config: map[string]string{"privateKey": "replacement-key"},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{toDelete.ID, toDisable.ID, toRotate.ID}, calls)
}

func TestProviderMutationFailsClosedWhenMarketCounterErrors(t *testing.T) {
	for _, operation := range []string{"delete", "disable", "rotate"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			svc := newHostpayProtectionTestService(client)
			instance := createHostpayProtectionTestInstance(t, svc, payment.TypeAlipay, true)
			hostpay.SetInFlightCounter(func(context.Context, int64) (int, error) {
				return 0, errors.New("market database unavailable")
			})
			t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })

			var err error
			switch operation {
			case "delete":
				err = svc.DeleteProviderInstance(ctx, instance.ID)
			case "disable":
				_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Enabled: hostpayProtectionBool(false)})
			case "rotate":
				_, err = svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Config: map[string]string{"appId": "new-app"}})
			}
			require.Error(t, err, "a failed market count must block destructive changes")
			require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
			require.Equal(t, "?", infraerrors.FromError(err).Metadata["market_count"])
			require.Equal(t, "0", infraerrors.FromError(err).Metadata["sub2api_count"])
			saved, loadErr := client.PaymentProviderInstance.Get(ctx, instance.ID)
			require.NoError(t, loadErr)
			require.Equal(t, instance.Config, saved.Config)
			require.True(t, saved.Enabled)
		})
	}
}

func TestNonOfficialProviderDoesNotCallMarketInFlightCounter(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := newHostpayProtectionTestService(client)
	calls := 0
	hostpay.SetInFlightCounter(func(context.Context, int64) (int, error) {
		calls++
		return 99, nil
	})
	t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })

	for _, providerKey := range []string{payment.TypeStripe, payment.TypeEasyPay, payment.TypeAirwallex} {
		instance := createHostpayProtectionTestInstance(t, svc, providerKey, false)
		_, err := svc.UpdateProviderInstance(ctx, instance.ID, UpdateProviderInstanceRequest{Enabled: hostpayProtectionBool(false)})
		require.NoError(t, err)
		require.NoError(t, svc.DeleteProviderInstance(ctx, instance.ID))
	}
	require.Zero(t, calls)
}

func TestProviderInFlightCountsIncludeSub2apiAndMarket(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := newHostpayProtectionTestService(client)
	instance := createHostpayProtectionTestInstance(t, svc, payment.TypeAlipay, true)
	user, err := client.User.Create().SetEmail("inflight@example.com").SetPasswordHash("hash").SetUsername("inflight").Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(10).SetPayAmount(10).SetRechargeCode("inflight-order").SetOutTradeNo("inflight-order").
		SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").SetStatus(payment.OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("test.local").
		SetProviderInstanceID(strconv.FormatInt(instance.ID, 10)).SetProviderKey(instance.ProviderKey).Save(ctx)
	require.NoError(t, err)
	hostpay.SetInFlightCounter(func(context.Context, int64) (int, error) { return 2, nil })
	t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })

	err = svc.DeleteProviderInstance(ctx, instance.ID)
	require.Equal(t, "PENDING_ORDERS", infraerrors.Reason(err))
	require.Equal(t, map[string]string{"count": "3", "sub2api_count": "1", "market_count": "2"}, infraerrors.FromError(err).Metadata)
}

func TestProviderKeyCannotChangeThroughUpdatePayload(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := newHostpayProtectionTestService(client)
	instance := createHostpayProtectionTestInstance(t, svc, payment.TypeAlipay, true)
	calls := 0
	hostpay.SetInFlightCounter(func(context.Context, int64) (int, error) {
		calls++
		return 1, nil
	})
	t.Cleanup(func() { hostpay.SetInFlightCounter(nil) })
	var req UpdateProviderInstanceRequest
	require.NoError(t, json.Unmarshal([]byte(`{"provider_key":"stripe"}`), &req))
	_, err := svc.UpdateProviderInstance(ctx, instance.ID, req)
	require.NoError(t, err, "provider_key is not patchable and should be ignored")
	require.Zero(t, calls, "an ignored provider_key alone is not a destructive update")
	saved, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, payment.TypeAlipay, saved.ProviderKey)
}

func newHostpayProtectionTestService(client *dbent.Client) *PaymentConfigService {
	return &PaymentConfigService{entClient: client}
}

func createHostpayProtectionTestInstance(t *testing.T, svc *PaymentConfigService, providerKey string, enabled bool) *dbent.PaymentProviderInstance {
	t.Helper()
	config := map[string]string{}
	if providerKey == payment.TypeAlipay {
		config = map[string]string{"appId": "test-app", "privateKey": "original-key"}
	}
	if providerKey == payment.TypeWxpay && enabled {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		privateDER, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		require.NoError(t, err)
		config = map[string]string{
			"appId": "test-app", "mchId": "test-merchant", "certSerial": "SERIAL",
			"apiV3Key": "0123456789abcdef0123456789abcdef", "publicKeyId": "PUB_KEY_ID_TEST",
			"privateKey": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
			"publicKey":  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		}
	}
	supportedTypes := []string{providerKey}
	if providerKey == payment.TypeEasyPay {
		supportedTypes = []string{payment.TypeAlipay}
	}
	instance, err := svc.CreateProviderInstance(context.Background(), CreateProviderInstanceRequest{
		ProviderKey: providerKey, Name: "hostpay-protection-test", Config: config,
		SupportedTypes: supportedTypes, Enabled: enabled,
	})
	require.NoError(t, err)
	return instance
}

func hostpayProtectionBool(value bool) *bool { return &value }
