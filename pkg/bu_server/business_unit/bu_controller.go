// Package business_unit implement the management functions of business unit.
package business_unit

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nuts-foundation/go-did/did"
	"github.com/openebl/openebl/pkg/bu_server/cert_authority"
	"github.com/openebl/openebl/pkg/bu_server/model"
	"github.com/openebl/openebl/pkg/bu_server/storage"
	"github.com/openebl/openebl/pkg/bu_server/webhook"
	"github.com/openebl/openebl/pkg/envelope"
	eblpkix "github.com/openebl/openebl/pkg/pkix"
)

// BusinessUnitManager is the interface that wraps the basic management functions of business unit.
type BusinessUnitManager interface {
	CreateBusinessUnit(ctx context.Context, ts int64, req CreateBusinessUnitRequest) (model.BusinessUnit, error)
	UpdateBusinessUnit(ctx context.Context, ts int64, req UpdateBusinessUnitRequest) (model.BusinessUnit, error)
	ListBusinessUnits(ctx context.Context, req storage.ListBusinessUnitsRequest) (storage.ListBusinessUnitsResult, error)
	SetStatus(ctx context.Context, ts int64, req SetBusinessUnitStatusRequest) (model.BusinessUnit, error)
	AddAuthentication(ctx context.Context, ts int64, req AddAuthenticationRequest) (model.BusinessUnitAuthentication, error)
	RevokeAuthentication(ctx context.Context, ts int64, req RevokeAuthenticationRequest) (model.BusinessUnitAuthentication, error)
	ListAuthentication(ctx context.Context, req storage.ListAuthenticationRequest) (storage.ListAuthenticationResult, error)
	GetJWSSigner(ctx context.Context, req GetJWSSignerRequest) (JWSSigner, error)
	GetJWEEncryptors(ctx context.Context, req GetJWEEncryptorsRequest) ([]JWEEncryptor, error)

	// ActivateAuthentication activates an authentication of a business unit with its certificate.
	// This function is NOT for REST API.
	// The returned error can be model.ErrAuthenticationNotFound, model.ErrAuthenticationNotPending, model.ErrInvalidParameter or any other errors.
	ActivateAuthentication(ctx context.Context, tx storage.Tx, ts int64, certRaw []byte) (model.BusinessUnitAuthentication, error)
}

type JWSSigner interface {
	// Public returns the public key corresponding to the opaque,
	// private key.
	Public() crypto.PublicKey

	// Sign signs digest with the private key, possibly using entropy from
	// rand. For an RSA key, the resulting signature should be either a
	// PKCS #1 v1.5 or PSS signature (as indicated by opts). For an (EC)DSA
	// key, it should be a DER-serialised, ASN.1 signature structure.
	//
	// Hash implements the SignerOpts interface and, in most cases, one can
	// simply pass in the hash function used as opts. Sign may also attempt
	// to type assert opts to other types in order to obtain algorithm
	// specific values. See the documentation in each package for details.
	//
	// Note that when a signature of a hash of a larger message is needed,
	// the caller is responsible for hashing the larger message and passing
	// the hash (as digest) and the hash function (as opts) to Sign.
	Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error)

	AvailableJWSSignAlgorithms() []envelope.SignatureAlgorithm
	Cert() []*x509.Certificate
}

type JWEEncryptor interface {
	Public() crypto.PublicKey
	AvailableJWEEncryptAlgorithms() []envelope.KeyEncryptionAlgorithm
}

// CreateBusinessUnitRequest is the request to create a business unit.
type CreateBusinessUnitRequest struct {
	Requester     string `json:"requester"`      // User who makes the request.
	ApplicationID string `json:"application_id"` // The ID of the application this BusinessUnit belongs to.

	Name         string                   `json:"name"`          // Name of the BusinessUnit.
	Addresses    []string                 `json:"addresses"`     // List of addresses associated with the BusinessUnit.
	Country      string                   `json:"country"`       // Country Code of the BusinessUnit. (Eg: US, TW, JP)
	Emails       []string                 `json:"emails"`        // List of emails associated with the BusinessUnit.
	PhoneNumbers []string                 `json:"phone_numbers"` // List of phone numbers associated with the BusinessUnit.
	Status       model.BusinessUnitStatus `json:"status"`        // Status of the application.
}

// UpdateBusinessUnitRequest is the request to update a business unit.
type UpdateBusinessUnitRequest struct {
	Requester     string   `json:"requester"`      // User who makes the request.
	ApplicationID string   `json:"application_id"` // The ID of the application this BusinessUnit belongs to.
	ID            did.DID  `json:"id"`             // Unique DID of a BusinessUnit.
	Name          string   `json:"name"`           // Name of the BusinessUnit.
	Addresses     []string `json:"addresses"`      // List of addresses associated with the BusinessUnit.
	Country       string   `json:"country"`        // Country Code of the BusinessUnit. (Eg: US, TW, JP)
	Emails        []string `json:"emails"`         // List of emails associated with the BusinessUnit.
	PhoneNumbers  []string `json:"phone_numbers"`  // List of phone numbers associated with the BusinessUnit.
}

// SetBusinessUnitStatusRequest is the request to set the status of a business unit.
type SetBusinessUnitStatusRequest struct {
	Requester     string                   `json:"requester"`      // User who makes the request.
	ApplicationID string                   `json:"application_id"` // The ID of the application this BusinessUnit belongs to.
	ID            did.DID                  `json:"id"`             // Unique DID of a BusinessUnit.
	Status        model.BusinessUnitStatus `json:"status"`         // Status of the application.
}

// AddAuthenticationRequest is the request to add an authentication to a business unit.
type AddAuthenticationRequest struct {
	Requester        string                   `json:"requester"`          // User who makes the request.
	ApplicationID    string                   `json:"application_id"`     // The ID of the application this BusinessUnit belongs to.
	BusinessUnitID   did.DID                  `json:"id"`                 // Unique DID of a BusinessUnit.
	PrivateKeyOption eblpkix.PrivateKeyOption `json:"private_key_option"` // Option of the private key.
}

// RevokeAuthenticationRequest is the request to revoke an authentication from a business unit.
type RevokeAuthenticationRequest struct {
	Requester        string  `json:"requester"`         // User who makes the request.
	ApplicationID    string  `json:"application_id"`    // The ID of the application this BusinessUnit belongs to.
	BusinessUnitID   did.DID `json:"id"`                // Unique DID of a BusinessUnit.
	AuthenticationID string  `json:"authentication_id"` // Unique ID of the authentication.
}

type GetJWSSignerRequest struct {
	ApplicationID    string  `json:"application_id"`    // The ID of the application this BusinessUnit belongs to.
	BusinessUnitID   did.DID `json:"id"`                // Unique DID of a BusinessUnit.
	AuthenticationID string  `json:"authentication_id"` // Unique ID of the authentication.
}

type GetJWEEncryptorsRequest struct {
	BusinessUnitIDs []string `json:"ids"` // Unique DID of a BusinessUnit.
}

type _BusinessUnitManager struct {
	ca          cert_authority.CertAuthority
	storage     storage.BusinessUnitStorage
	webhookCtrl webhook.WebhookController
	jwtFactory  JWTFactory
}

func NewBusinessUnitManager(storage storage.BusinessUnitStorage, ca cert_authority.CertAuthority, webhookCtrl webhook.WebhookController, jwtFactory JWTFactory) BusinessUnitManager {
	return &_BusinessUnitManager{
		ca:          ca,
		storage:     storage,
		webhookCtrl: webhookCtrl,
		jwtFactory:  jwtFactory,
	}
}

func (m *_BusinessUnitManager) CreateBusinessUnit(ctx context.Context, ts int64, req CreateBusinessUnitRequest) (model.BusinessUnit, error) {
	if err := ValidateCreateBusinessUnitRequest(req); err != nil {
		return model.BusinessUnit{}, err
	}

	bu := model.BusinessUnit{
		ID: did.DID{
			Method: model.DIDMethod,
			ID:     uuid.NewString(),
		},
		ApplicationID: req.ApplicationID,
		Version:       1,
		Status:        req.Status,
		Name:          req.Name,
		Addresses:     req.Addresses,
		Country:       req.Country,
		Emails:        req.Emails,
		PhoneNumbers:  req.PhoneNumbers,
		CreatedAt:     ts,
		CreatedBy:     req.Requester,
		UpdatedAt:     ts,
		UpdatedBy:     req.Requester,
	}

	tx, ctx, err := m.storage.CreateTx(ctx, storage.TxOptionWithWrite(true), storage.TxOptionWithIsolationLevel(sql.LevelSerializable))
	if err != nil {
		return model.BusinessUnit{}, err
	}
	defer tx.Rollback(ctx)

	if err := m.storage.StoreBusinessUnit(ctx, tx, bu); err != nil {
		return model.BusinessUnit{}, err
	}

	if err = m.webhookCtrl.SendWebhookEvent(ctx, tx, ts, req.ApplicationID, bu.ID.String(), model.WebhookEventBUCreated); err != nil {
		return model.BusinessUnit{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BusinessUnit{}, err
	}
	return bu, nil
}

func (m *_BusinessUnitManager) UpdateBusinessUnit(ctx context.Context, ts int64, req UpdateBusinessUnitRequest) (model.BusinessUnit, error) {
	if err := ValidateUpdateBusinessUnitRequest(req); err != nil {
		return model.BusinessUnit{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx, storage.TxOptionWithWrite(true), storage.TxOptionWithIsolationLevel(sql.LevelSerializable))
	if err != nil {
		return model.BusinessUnit{}, err
	}
	defer tx.Rollback(ctx)

	bu, err := m.getBusinessUnit(ctx, tx, req.ApplicationID, req.ID)
	if err != nil {
		return model.BusinessUnit{}, err
	}
	bu.Version += 1
	bu.Name = req.Name
	bu.Addresses = req.Addresses
	bu.Country = req.Country
	bu.Emails = req.Emails
	bu.PhoneNumbers = req.PhoneNumbers
	bu.UpdatedAt = ts
	bu.UpdatedBy = req.Requester

	if err := m.storage.StoreBusinessUnit(ctx, tx, bu); err != nil {
		return model.BusinessUnit{}, err
	}

	if err = m.webhookCtrl.SendWebhookEvent(ctx, tx, ts, req.ApplicationID, bu.ID.String(), model.WebhookEventBUUpdated); err != nil {
		return model.BusinessUnit{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BusinessUnit{}, err
	}
	return bu, nil
}

func (m *_BusinessUnitManager) ListBusinessUnits(ctx context.Context, req storage.ListBusinessUnitsRequest) (storage.ListBusinessUnitsResult, error) {
	if err := ValidateListBusinessUnitRequest(req); err != nil {
		return storage.ListBusinessUnitsResult{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx)
	if err != nil {
		return storage.ListBusinessUnitsResult{}, err
	}
	defer tx.Rollback(ctx)

	result, err := m.storage.ListBusinessUnits(ctx, tx, req)
	if err != nil {
		return storage.ListBusinessUnitsResult{}, err
	}
	for i := range result.Records {
		for j := range result.Records[i].Authentications {
			result.Records[i].Authentications[j].PrivateKey = "" // Erase PrivateKey before returning.
		}
	}
	return result, err
}

func (m *_BusinessUnitManager) SetStatus(ctx context.Context, ts int64, req SetBusinessUnitStatusRequest) (model.BusinessUnit, error) {
	if err := ValidateSetBusinessUnitStatusRequest(req); err != nil {
		return model.BusinessUnit{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx, storage.TxOptionWithWrite(true), storage.TxOptionWithIsolationLevel(sql.LevelSerializable))
	if err != nil {
		return model.BusinessUnit{}, err
	}
	defer tx.Rollback(ctx)

	listReq := storage.ListBusinessUnitsRequest{
		Limit:         1,
		ApplicationID: req.ApplicationID,
		BusinessUnitIDs: []string{
			req.ID.String(),
		},
	}
	listResult, err := m.storage.ListBusinessUnits(ctx, tx, listReq)
	if err != nil {
		return model.BusinessUnit{}, err
	}
	if len(listResult.Records) == 0 {
		return model.BusinessUnit{}, model.ErrBusinessUnitNotFound
	}

	bu := listResult.Records[0].BusinessUnit
	bu.Version += 1
	bu.Status = req.Status
	bu.UpdatedAt = ts
	bu.UpdatedBy = req.Requester

	if err := m.storage.StoreBusinessUnit(ctx, tx, bu); err != nil {
		return model.BusinessUnit{}, err
	}

	if err = m.webhookCtrl.SendWebhookEvent(ctx, tx, ts, req.ApplicationID, bu.ID.String(), model.WebhookEventBUUpdated); err != nil {
		return model.BusinessUnit{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BusinessUnit{}, err
	}
	return bu, nil
}

func (m *_BusinessUnitManager) AddAuthentication(ctx context.Context, ts int64, req AddAuthenticationRequest) (model.BusinessUnitAuthentication, error) {
	if err := ValidateAddAuthenticationRequest(req); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	privateKey, err := m.createPrivateKey(ctx, req.PrivateKeyOption)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx, storage.TxOptionWithWrite(true), storage.TxOptionWithIsolationLevel(sql.LevelSerializable))
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	defer tx.Rollback(ctx)

	oldBu, err := m.getBusinessUnit(ctx, tx, req.ApplicationID, req.BusinessUnitID)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	if oldBu.Status != model.BusinessUnitStatusActive {
		return model.BusinessUnitAuthentication{}, model.ErrBusinessUnitInActive
	}

	csrRaw, err := eblpkix.CreateCertificateSigningRequest(
		privateKey,
		[]string{oldBu.Country},
		[]string{oldBu.Name},
		nil,
		oldBu.ID.String(),
	)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	auth := model.BusinessUnitAuthentication{
		ID:                        uuid.NewString(),
		Version:                   1,
		BusinessUnit:              req.BusinessUnitID,
		Status:                    model.BusinessUnitAuthenticationStatusPending,
		CreatedAt:                 ts,
		CreatedBy:                 req.Requester,
		PublicKeyID:               eblpkix.GetPublicKeyID(eblpkix.GetPublicKey(privateKey)),
		CertificateSigningRequest: string(csrRaw),
	}
	auth.PrivateKey, err = eblpkix.MarshalPrivateKey(privateKey)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	if err := m.storage.StoreAuthentication(ctx, tx, auth); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	if err = m.webhookCtrl.SendWebhookEvent(ctx, tx, ts, req.ApplicationID, auth.ID, model.WebhookEventAuthCreated); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	auth.PrivateKey = "" // Erase PrivateKey before returning.
	return auth, nil
}

func (m *_BusinessUnitManager) ActivateAuthentication(ctx context.Context, tx storage.Tx, ts int64, certRaw []byte) (model.BusinessUnitAuthentication, error) {
	certs, err := eblpkix.ParseCertificate(certRaw)
	if err != nil {
		return model.BusinessUnitAuthentication{}, fmt.Errorf("%s: %w", err.Error(), model.ErrInvalidParameter)
	}
	if len(certs) == 0 || certs[0].SerialNumber == nil || certs[0].AuthorityKeyId == nil {
		return model.BusinessUnitAuthentication{}, fmt.Errorf("certificate is empty, or serial number is not available, or authority key id is not available: %w", model.ErrInvalidParameter)
	}

	// TODO: Validate the certificate.

	pubKeyID := eblpkix.GetSubjectKeyIDFromCertificate(certs[0])
	issuerKeyID := hex.EncodeToString(certs[0].AuthorityKeyId)
	certSerialNumber := certs[0].SerialNumber.String()

	listReq := storage.ListAuthenticationRequest{
		Limit:        1,
		PublicKeyIDs: []string{pubKeyID},
	}
	listResult, err := m.storage.ListAuthentication(ctx, tx, listReq)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	if len(listResult.Records) == 0 {
		return model.BusinessUnitAuthentication{}, model.ErrAuthenticationNotFound
	}

	buAuth := listResult.Records[0]
	if buAuth.Status != model.BusinessUnitAuthenticationStatusPending {
		return model.BusinessUnitAuthentication{}, model.ErrAuthenticationNotPending
	}

	buAuth.Version += 1
	buAuth.Status = model.BusinessUnitAuthenticationStatusActive
	buAuth.Certificate = string(certRaw)
	buAuth.CertificateSerialNumber = certSerialNumber
	buAuth.IssuerKeyID = issuerKeyID
	buAuth.ActivatedAt = ts

	if err := m.storage.StoreAuthentication(ctx, tx, buAuth); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	return buAuth, nil
}

func (m *_BusinessUnitManager) RevokeAuthentication(ctx context.Context, ts int64, req RevokeAuthenticationRequest) (model.BusinessUnitAuthentication, error) {
	if err := ValidateRevokeAuthenticationRequest(req); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx, storage.TxOptionWithWrite(true), storage.TxOptionWithIsolationLevel(sql.LevelSerializable))
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	defer tx.Rollback(ctx)

	listReq := storage.ListAuthenticationRequest{
		Limit:          1,
		ApplicationID:  req.ApplicationID,
		BusinessUnitID: req.BusinessUnitID.String(),
		AuthenticationIDs: []string{
			req.AuthenticationID,
		},
	}
	listResult, err := m.storage.ListAuthentication(ctx, tx, listReq)
	if err != nil {
		return model.BusinessUnitAuthentication{}, err
	}
	if len(listResult.Records) == 0 {
		return model.BusinessUnitAuthentication{}, model.ErrAuthenticationNotFound
	}

	auth := listResult.Records[0]
	if auth.Status == model.BusinessUnitAuthenticationStatusRevoked {
		auth.PrivateKey = "" // Erase PrivateKey before returning.
		return auth, nil     // Already revoked.
	}

	auth.Version += 1
	auth.Status = model.BusinessUnitAuthenticationStatusRevoked
	auth.RevokedAt = ts
	auth.RevokedBy = req.Requester

	if err := m.storage.StoreAuthentication(ctx, tx, auth); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	if err = m.webhookCtrl.SendWebhookEvent(ctx, tx, ts, req.ApplicationID, auth.ID, model.WebhookEventAuthRevoked); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.BusinessUnitAuthentication{}, err
	}

	auth.PrivateKey = "" // Erase PrivateKey before returning.
	return auth, nil
}

func (m *_BusinessUnitManager) ListAuthentication(ctx context.Context, req storage.ListAuthenticationRequest) (storage.ListAuthenticationResult, error) {
	if err := ValidateListAuthenticationRequest(req); err != nil {
		return storage.ListAuthenticationResult{}, err
	}

	tx, ctx, err := m.storage.CreateTx(ctx)
	if err != nil {
		return storage.ListAuthenticationResult{}, err
	}
	defer tx.Rollback(ctx)

	result, err := m.storage.ListAuthentication(ctx, tx, req)
	if err != nil {
		return storage.ListAuthenticationResult{}, err
	}

	for i := range result.Records {
		result.Records[i].PrivateKey = "" // Erase PrivateKey before returning.
	}
	return result, nil
}

func (m *_BusinessUnitManager) GetJWSSigner(ctx context.Context, req GetJWSSignerRequest) (JWSSigner, error) {
	auth, err := func() (model.BusinessUnitAuthentication, error) {
		tx, ctx, err := m.storage.CreateTx(ctx)
		if err != nil {
			return model.BusinessUnitAuthentication{}, err
		}
		defer tx.Rollback(ctx)

		listReq := storage.ListAuthenticationRequest{
			Limit:          1,
			ApplicationID:  req.ApplicationID,
			BusinessUnitID: req.BusinessUnitID.String(),
			AuthenticationIDs: []string{
				req.AuthenticationID,
			},
		}
		listResult, err := m.storage.ListAuthentication(ctx, tx, listReq)
		if err != nil {
			return model.BusinessUnitAuthentication{}, err
		}
		if len(listResult.Records) == 0 {
			return model.BusinessUnitAuthentication{}, model.ErrAuthenticationNotFound
		}

		return listResult.Records[0], nil
	}()

	if err != nil {
		return nil, err
	}

	if auth.Status != model.BusinessUnitAuthenticationStatusActive {
		return nil, model.ErrAuthenticationNotActive
	}

	if m.jwtFactory == nil {
		return DefaultJWTFactory.NewJWSSigner(auth)
	}
	return m.jwtFactory.NewJWSSigner(auth)
}

func (m *_BusinessUnitManager) GetJWEEncryptors(ctx context.Context, req GetJWEEncryptorsRequest) ([]JWEEncryptor, error) {
	getAuthentications := func(ctx context.Context, businessUnitIDs []string) (map[string]model.BusinessUnitAuthentication, error) {
		tx, ctx, err := m.storage.CreateTx(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)

		listReq := storage.ListBusinessUnitsRequest{
			Limit:           len(businessUnitIDs),
			BusinessUnitIDs: businessUnitIDs,
		}
		result, err := m.storage.ListBusinessUnits(ctx, tx, listReq)
		if err != nil {
			return nil, err
		}

		// find latest active authentications
		authenticates := make(map[string]model.BusinessUnitAuthentication)
		for _, record := range result.Records {
			for i := len(record.Authentications) - 1; i >= 0; i-- {
				if record.Authentications[i].Status == model.BusinessUnitAuthenticationStatusActive {
					authenticates[record.BusinessUnit.ID.String()] = record.Authentications[i]
					break
				}
			}
		}
		return authenticates, nil
	}

	authenticates, err := getAuthentications(ctx, req.BusinessUnitIDs)
	if err != nil {
		return nil, err
	}
	if len(authenticates) == 0 {
		return nil, model.ErrAuthenticationNotFound
	}
	if len(authenticates) != len(req.BusinessUnitIDs) {
		return nil, model.ErrAuthenticationNotActive
	}

	factory := m.jwtFactory
	if factory == nil {
		factory = DefaultJWTFactory
	}
	encryptors := make([]JWEEncryptor, 0, len(authenticates))
	for _, auth := range authenticates {
		encryptor, err := factory.NewJWEEncryptor(auth)
		if err != nil {
			return nil, err
		}
		encryptors = append(encryptors, encryptor)
	}
	return encryptors, nil
}

func (m *_BusinessUnitManager) getBusinessUnit(ctx context.Context, tx storage.Tx, appID string, id did.DID) (model.BusinessUnit, error) {
	if tx == nil {
		newTx, ctx, err := m.storage.CreateTx(ctx)
		if err != nil {
			return model.BusinessUnit{}, err
		}
		defer newTx.Rollback(ctx)
		tx = newTx
	}
	req := storage.ListBusinessUnitsRequest{
		Limit:         1,
		ApplicationID: appID,
		BusinessUnitIDs: []string{
			id.String(),
		},
	}
	result, err := m.storage.ListBusinessUnits(ctx, tx, req)
	if err != nil {
		return model.BusinessUnit{}, err
	}
	if len(result.Records) == 0 {
		return model.BusinessUnit{}, model.ErrBusinessUnitNotFound
	}
	return result.Records[0].BusinessUnit, nil
}

// createPrivateKey will return a private key. The type will be *rsa.PrivateKey or *ecdsa.PrivateKey.
func (m *_BusinessUnitManager) createPrivateKey(ctx context.Context, opt eblpkix.PrivateKeyOption) (any, error) {
	switch opt.KeyType {
	case eblpkix.PrivateKeyTypeRSA:
		return rsa.GenerateKey(rand.Reader, opt.BitLength)
	case eblpkix.PrivateKeyTypeECDSA:
		switch opt.CurveType {
		case eblpkix.ECDSACurveTypeP256:
			return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		case eblpkix.ECDSACurveTypeP384:
			return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		case eblpkix.ECDSACurveTypeP521:
			return ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
		default:
			return nil, model.ErrInvalidParameter
		}
	default:
		return nil, model.ErrInvalidParameter
	}
}

func (m *_BusinessUnitManager) createCertificateRequest(ctx context.Context, privateKey interface{}, bu model.BusinessUnit) ([]byte, error) {
	return eblpkix.CreateCertificateSigningRequest(
		privateKey,
		[]string{bu.Country},
		[]string{bu.Name},
		nil,
		bu.ID.String(),
	)
}
