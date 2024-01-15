package http

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/breez/notify/config"
	"github.com/breez/notify/notify"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type MobilePushWebHookQuery struct {
	Platform string  `form:"platform" binding:"required,oneof=ios android"`
	Token    string  `form:"token" binding:"required"`
	AppData  *string `form:"app_data"`
}

type NotificationConvertible interface {
	ToNotification(query *MobilePushWebHookQuery) *notify.Notification
}

type WebhookCallbackMessagePayload struct {
	Template    string `json:"template" binding:"required,eq=webhook_callback_message"`
	MessageType string `json:"message_type" binding:"required"`
	Data        struct {
		CallbackURL    string `json:"callback_url" binding:"required"`
		MessagePayload string `json:"message_payload"`
	} `json:"data"`
}

func (p *WebhookCallbackMessagePayload) ToNotification(query *MobilePushWebHookQuery) *notify.Notification {
	return &notify.Notification{
		Template:         p.Template,
		DisplayMessage:   p.GenerateDisplayMessage(),
		Type:             query.Platform,
		TargetIdentifier: query.Token,
		AppData:          query.AppData,
		Data: map[string]string{
			"callback_url":    p.Data.CallbackURL,
			"message_payload": p.Data.MessagePayload,
		},
	}
}

func (p *WebhookCallbackMessagePayload) GenerateDisplayMessage() string {
	switch p.MessageType {
	case "lnurlpay_info":
		return "Receiving payment"
	case "lnurlpay_invoice":
		return "Invoice requested"
	}
	return ""
}

type PaymentReceivedPayload struct {
	Template string `json:"template" binding:"required,eq=payment_received"`
	Data     struct {
		PaymentHash string `json:"payment_hash" binding:"required"`
	} `json:"data"`
}

func (p *PaymentReceivedPayload) ToNotification(query *MobilePushWebHookQuery) *notify.Notification {
	return &notify.Notification{
		Template:         p.Template,
		DisplayMessage:   "Incoming payment",
		Type:             query.Platform,
		TargetIdentifier: query.Token,
		AppData:          query.AppData,
		Data:             map[string]string{"payment_hash": p.Data.PaymentHash},
	}
}

type TxConfirmedPayload struct {
	Template string `json:"template" binding:"required,eq=tx_confirmed"`
	Data     struct {
		TxID string `json:"tx_id" binding:"required"`
	} `json:"data"`
}

func (p *TxConfirmedPayload) ToNotification(query *MobilePushWebHookQuery) *notify.Notification {
	return &notify.Notification{
		Template:         p.Template,
		DisplayMessage:   "Transaction confirmed",
		Type:             query.Platform,
		TargetIdentifier: query.Token,
		AppData:          query.AppData,
		Data:             map[string]string{"tx_id": p.Data.TxID},
	}
}

type AddressTxsChangedPayload struct {
	Template string `json:"template" binding:"required,eq=address_txs_changed"`
	Data     struct {
		Address string `json:"address" binding:"required"`
	} `json:"data"`
}

func (p *AddressTxsChangedPayload) ToNotification(query *MobilePushWebHookQuery) *notify.Notification {
	return &notify.Notification{
		Template:         p.Template,
		DisplayMessage:   "Address transactions changed",
		Type:             query.Platform,
		TargetIdentifier: query.Token,
		AppData:          query.AppData,
		Data:             map[string]string{"address": p.Data.Address},
	}
}

func Run(notifier *notify.Notifier, config *config.HTTPConfig) error {
	r := setupRouter(notifier)
	r.SetTrustedProxies(nil)
	return r.Run(config.Address)
}

func setupRouter(notifier *notify.Notifier) *gin.Engine {
	r := gin.Default()
	router := r.Group("api/v1")
	addWebHookRouter(router, notifier)
	return r
}

func addWebHookRouter(r *gin.RouterGroup, notifier *notify.Notifier) {
	r.POST("/notify", func(c *gin.Context) {

		body, _ := io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		// Make sure the query string fits the mobile push structure
		var query MobilePushWebHookQuery
		if err := c.ShouldBindQuery(&query); err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		// Find a matching notification payload
		payloads := []NotificationConvertible{&PaymentReceivedPayload{}, &TxConfirmedPayload{}, &AddressTxsChangedPayload{}, &WebhookCallbackMessagePayload{}}
		var validPayload NotificationConvertible
		for _, p := range payloads {
			if err := c.ShouldBindBodyWith(p, binding.JSON); err != nil {
				continue
			}
			validPayload = p
			break
		}

		if validPayload == nil {
			log.Printf("invalid payload, body: %s", body)
			c.AbortWithError(http.StatusBadRequest, fmt.Errorf("unsupported payload, body: %s", body))
			return
		}

		if err := notifier.Notify(c, validPayload.ToNotification(&query)); err != nil {
			log.Printf("failed to notify, query: %v, error: %v", query, err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.Status(http.StatusOK)
	})
}
