package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wssto2/go-core/apperr"
	"github.com/wssto2/go-core/database"
	"github.com/wssto2/go-core/event"
	"github.com/wssto2/go-core/gocore"
	"github.com/wssto2/go-core/identity/account"
	"github.com/wssto2/go-core/notification/push"
	"gorm.io/gorm"
)

// The limits of a device (NOTIF-DEVICE-001).
const (
	LabelMax = 100
	// MaxConsecutiveFailures disables a device after this many pushes that failed through its fault with no success
	// between; the push service answering 404 or 410 removes it at once.
	MaxConsecutiveFailures = 5
)

// The reasons of the errors the devices answer with; the client translates them.
const (
	// ReasonPushDisabled: the application was not given WithPush (400).
	ReasonPushDisabled apperr.Reason = "notification.push.disabled"
	// ReasonInvalidSubscription: the subscription cannot be pushed to: not https, bad keys (400).
	ReasonInvalidSubscription apperr.Reason = "notification.push.invalid_subscription"
	// ReasonDeviceDisabled: the device was disabled after repeated failures; registering again enables it (400).
	ReasonDeviceDisabled apperr.Reason = "notification.device.disabled"
	// ReasonDeviceUnknown: the person has no such device (404).
	ReasonDeviceUnknown apperr.Reason = "notification.device.unknown"
)

// PushConfig is what the app needs to offer push: whether it is on, and the key to subscribe the browser with.
type PushConfig struct {
	Enabled bool `json:"enabled"`
	// PublicKey is the VAPID application server key, base64url; empty while push is off.
	PublicKey string `json:"public_key"` //nolint:tagliatelle // snake_case wire
}

// Device is one browser or installed app of a person that enabled push. The endpoint and keys are never sent:
// EndpointHash lets the app recognise its own browser in the list.
type Device struct {
	ID           int    `json:"id"`
	Label        string `json:"label"`
	Platform     string `json:"platform"`
	EndpointHash string `json:"endpoint_hash"` //nolint:tagliatelle // snake_case wire
	// Active is false after repeated failures; registering the browser again enables it.
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"created_at"`      //nolint:tagliatelle // snake_case wire
	LastSeenAt    time.Time  `json:"last_seen_at"`    //nolint:tagliatelle // snake_case wire
	LastSuccessAt *time.Time `json:"last_success_at"` //nolint:tagliatelle // snake_case wire
}

// DeviceList is a person's devices, most recently seen first.
type DeviceList struct {
	Items []Device `json:"items"`
}

// Subscription is the browser's Web Push subscription as the Push API reports it.
type Subscription struct {
	Endpoint string `json:"endpoint" validation:"required|max:1024"`
	P256DH   string `json:"p256dh"   validation:"required|max:128"`
	Auth     string `json:"auth"     validation:"required|max:64"`
}

// Devices is what a person does with their own push devices (NOTIF-DEVICE-001): register this browser, list, rename,
// remove and test them. Every method acts on one person's devices, whose id the caller takes from the session, never
// from the request.
type Devices struct {
	store  *store
	sender *push.Sender
	clock  gocore.Clock
	db     *gorm.DB
}

// Config says whether push is on and the key to subscribe with.
func (d *Devices) Config() PushConfig {
	if d.sender == nil {
		return PushConfig{} //nolint:exhaustruct,exhaustruct_v5 // zero by design
	}

	return PushConfig{Enabled: true, PublicKey: d.sender.PublicKey()}
}

// List returns the person's devices, most recently seen first.
func (d *Devices) List(ctx context.Context, userID int) (DeviceList, error) {
	rows, err := d.store.devicesOfUser(ctx, userID)
	if err != nil {
		return DeviceList{}, apperr.Internal(err)
	}

	list := DeviceList{Items: make([]Device, 0, len(rows))}
	for _, r := range rows {
		list.Items = append(list.Items, r.device())
	}

	return list, nil
}

// Register saves this browser's subscription for the person and returns the device. It is an upsert by endpoint
// (NOTIF-DEVICE-001, rules 2 and 3): a browser that registers again, as the app does at every start, keeps its one row,
// refreshed; a disabled device is enabled again; an endpoint another person registered moves to this one, and the
// pushes still queued for the earlier person are cancelled. userAgent names a new device and its platform; a label the
// same person chose stays.
func (d *Devices) Register(
	ctx context.Context, userID int, userAgent string, subscription Subscription,
) (Device, error) {
	if d.sender == nil {
		return Device{}, apperr.BadRequest("push notifications are not configured").WithReason(ReasonPushDisabled)
	}

	sub := push.Subscription{
		Endpoint: strings.TrimSpace(subscription.Endpoint),
		P256DH:   strings.TrimSpace(subscription.P256DH),
		Auth:     strings.TrimSpace(subscription.Auth),
	}

	err := sub.Validate()
	if err != nil {
		return Device{}, apperr.BadRequest("invalid push subscription").WithReason(ReasonInvalidSubscription)
	}

	now := whole(d.clock.Now())
	next := deviceRow{
		ID:            0,
		UserID:        uint32(userID), //nolint:gosec // a session's account id
		Endpoint:      sub.Endpoint,
		EndpointHash:  EndpointHash(sub.Endpoint),
		P256DH:        sub.P256DH,
		Auth:          sub.Auth,
		Label:         cleanLabel(account.DeviceLabel(userAgent)),
		Platform:      platformOf(userAgent),
		CreatedAt:     now,
		LastSeenAt:    now,
		LastSuccessAt: nil,
		FailureCount:  0,
		DisabledAt:    nil,
		UpdatedAt:     now,
	}

	saved, err := d.store.registerDevice(ctx, next, now)
	if err != nil {
		return Device{}, apperr.Internal(err)
	}

	return saved.device(), nil
}

// Rename changes the label of one of the person's devices.
func (d *Devices) Rename(ctx context.Context, userID, deviceID int, label string) (Device, error) {
	label = cleanLabel(label)
	if label == "" {
		return Device{}, apperr.BadRequest("the label is empty")
	}

	_, err := d.owned(ctx, userID, deviceID)
	if err != nil {
		return Device{}, err
	}

	// The label alone, on the person's own device: a push result written meanwhile stands.
	err = d.store.renameDevice(ctx, userID, uint64(deviceID), label, whole(d.clock.Now())) //nolint:gosec // see owned
	if err != nil {
		return Device{}, apperr.Internal(err)
	}

	r, err := d.owned(ctx, userID, deviceID)
	if err != nil {
		return Device{}, err
	}

	return r.device(), nil
}

// Remove deletes one of the person's devices and cancels the pushes queued for it (NOTIF-DEVICE-001, rule 4).
func (d *Devices) Remove(ctx context.Context, userID, deviceID int) error {
	_, err := d.owned(ctx, userID, deviceID)
	if err != nil {
		return err
	}

	return d.delete(ctx, userID, deviceID)
}

// Unregister deletes the person's device with the given endpoint, as sign-out does. An endpoint that is not theirs is
// left alone: it already moved to whoever signed in on that browser since; an unknown one is fine.
func (d *Devices) Unregister(ctx context.Context, userID int, endpoint string) error {
	dev, ok, err := d.store.deviceByHash(ctx, EndpointHash(strings.TrimSpace(endpoint)))
	if err != nil {
		return apperr.Internal(err)
	}

	if !ok || int(dev.UserID) != userID {
		return nil
	}

	return d.delete(ctx, userID, int(dev.ID)) //nolint:gosec // an auto-increment id fits an int
}

// SendTest pushes a test notification to one of the person's devices only, through the whole path a real one takes: the
// queue, the notification, the delivery worker.
func (d *Devices) SendTest(ctx context.Context, userID, deviceID int) error {
	if d.sender == nil {
		return apperr.BadRequest("push notifications are not configured").WithReason(ReasonPushDisabled)
	}

	r, err := d.owned(ctx, userID, deviceID)
	if err != nil {
		return err
	}

	if r.DisabledAt != nil {
		return apperr.BadRequest("the device is disabled").WithReason(ReasonDeviceDisabled)
	}

	err = database.NewTransactor(d.db).WithinTransaction(ctx, func(ctx context.Context) error {
		return deviceTested.Publish(ctx, deviceTestPayload{UserID: userID, DeviceID: deviceID})
	})
	if err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// owned is the person's device, or "not found": another person's device is not theirs to see.
func (d *Devices) owned(ctx context.Context, userID, deviceID int) (deviceRow, error) {
	dev, ok, err := d.store.deviceByID(ctx, deviceKey(deviceID))
	if err != nil {
		return deviceRow{}, apperr.Internal(err)
	}

	if !ok || int(dev.UserID) != userID {
		return deviceRow{}, unknownDevice()
	}

	return dev, nil
}

func (d *Devices) delete(ctx context.Context, userID, deviceID int) error {
	err := d.store.removeDevice(ctx, userID, deviceKey(deviceID), "device removed", whole(d.clock.Now()))
	if err != nil {
		return apperr.Internal(err)
	}

	return nil
}

// deviceKey is a path id as the table's key: the route validates it as at least 1.
func deviceKey(id int) uint64 { return uint64(id) } //nolint:gosec // validated min:1 by the route

func unknownDevice() error {
	return apperr.NotFound("device not found").WithReason(ReasonDeviceUnknown)
}

func (r deviceRow) device() Device {
	return Device{
		ID:            int(r.ID), //nolint:gosec // an auto-increment id fits an int
		Label:         r.Label,
		Platform:      r.Platform,
		EndpointHash:  r.EndpointHash,
		Active:        r.DisabledAt == nil,
		CreatedAt:     r.CreatedAt,
		LastSeenAt:    r.LastSeenAt,
		LastSuccessAt: r.LastSuccessAt,
	}
}

func (r deviceRow) subscription() push.Subscription {
	return push.Subscription{Endpoint: r.Endpoint, P256DH: r.P256DH, Auth: r.Auth}
}

// EndpointHash is the stable key of a subscription endpoint, hex SHA-256: the endpoint is a capability URL and is never
// shown to a client, so the hash is how the app recognises its own device.
func EndpointHash(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))

	return hex.EncodeToString(sum[:])
}

// cleanLabel trims a label to one line of at most LabelMax characters.
func cleanLabel(label string) string {
	label = strings.Join(strings.Fields(label), " ")

	if utf8.RuneCountInString(label) > LabelMax {
		label = string([]rune(label)[:LabelMax])
	}

	return label
}

// platformOf names the platform of a User-Agent, for the icon of a device.
func platformOf(userAgent string) string {
	for _, p := range []struct{ token, name string }{
		{"iPhone", "ios"}, {"iPad", "ios"}, {"Android", "android"},
		{"Macintosh", "macos"}, {"Windows", "windows"}, {"Linux", "linux"},
	} {
		if strings.Contains(userAgent, p.token) {
			return p.name
		}
	}

	return ""
}

// deviceTestPayload is the module's own event: a person asked to test one of their devices.
type deviceTestPayload struct {
	UserID   int `json:"user_id"`   //nolint:tagliatelle // snake_case wire
	DeviceID int `json:"device_id"` //nolint:tagliatelle // snake_case wire
}

// deviceTested is published by the device test route; its consumer sends the test notification to that device.
//
//nolint:gochecknoglobals // an event is declared once, like notification.test
var deviceTested = event.Define[deviceTestPayload]("notification.device-test")

// deviceTestConsumer sends the test notification to the one device the person asked about.
func (n *Notices) deviceTestConsumer() event.Consumer {
	return deviceTested.To("notification.device-test", func(ctx context.Context, p deviceTestPayload) error {
		return n.send(ctx, systemTest, To(p.UserID), func(Recipient) Message {
			//nolint:exhaustruct,exhaustruct_v5 // zero by design
			return Message{Title: "Test notification", Body: "If you see this, notifications reach this device."}
		}, uint64(p.DeviceID)) //nolint:gosec // a device id the route checked
	})
}
