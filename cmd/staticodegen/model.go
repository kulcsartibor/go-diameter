// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package main

// Wire kind of an AVP field after dictionary resolution.
type kind int

const (
	kindU32   kind = iota // Unsigned32 → uint32
	kindU64               // Unsigned64 → uint64
	kindEnum              // Enumerated/Integer32 → int32
	kindTime              // Time → uint32 (raw NTP seconds, no time.Time alloc)
	kindBytes             // UTF8String/OctetString/DiameterIdentity/Address → []byte
	kindGroup             // Grouped → nested generated struct
)

// field is one struct field in a generated type. Order in the fields
// slice is the AVP emission order (codec-design.md §2.6).
type field struct {
	goName   string // Go field name
	avpName  string // dictionary AVP name (resolution key)
	repeated bool
	group    string // generated struct type name when the AVP is Grouped

	// filled by resolve():
	code     uint32
	vendorID uint32
	flags    byte
	kind     kind
}

// structDef is one generated type: either a grouped AVP or a message body.
type structDef struct {
	name    string
	avpName string
	doc     string
	fields  []field
}

// messageDef is a generated message type (fixed command code / app id).
type messageDef struct {
	structDef
	code  uint32
	appID uint32
}

// enumAVP names an Enumerated AVP whose dictionary items are emitted as
// Go constants, with the constant name prefix.
type enumAVP struct {
	avpName string
	prefix  string
}

// application is one Diameter application emitted into its own Go package
// under the staticodec/ root (staticodec/<pkg>).
type application struct {
	pkg      string // Go package name / output subdir
	doc      string // package doc line
	groups   []structDef
	messages []messageDef
	enums    []enumAVP
}

// apps is the set of applications the generator emits. Ro is not listed
// separately: it reuses Gy's CCR/CCA (same command 272), so the gy package
// serves both.
var apps = []application{gyApp, syApp}

// ---- Gy (DCCA, RFC 4006 + 3GPP) — also serves Ro -----------------------

var gyApp = application{
	pkg: "gy",
	doc: "Package gy is the generated static codec for Diameter Gy/DCCA\n" +
		"(Application-Id 4, command 272: CCR/CCA). Ro reuses the same messages.",
	enums: []enumAVP{
		{"CC-Request-Type", "CCRequestType"},
		{"CC-Session-Failover", "CCSessionFailover"},
		{"Credit-Control-Failure-Handling", "CreditControlFailureHandling"},
		{"Subscription-Id-Type", "SubscriptionIDType"},
		{"User-Equipment-Info-Type", "UserEquipmentInfoType"},
		{"Termination-Cause", "TerminationCause"},
		{"Tariff-Change-Usage", "TariffChangeUsage"},
		{"Final-Unit-Action", "FinalUnitAction"},
		{"Trigger-Type", "TriggerType"},
		{"TGPP-PDP-Type", "TGPPPDPType"},
	},
	groups: []structDef{
		{
			name: "SubscriptionID", avpName: "Subscription-Id",
			doc: "SubscriptionID is the Subscription-Id grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "Type", avpName: "Subscription-Id-Type"},
				{goName: "Data", avpName: "Subscription-Id-Data"},
			},
		},
		{
			name: "UserEquipmentInfo", avpName: "User-Equipment-Info",
			doc: "UserEquipmentInfo is the User-Equipment-Info grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "Type", avpName: "User-Equipment-Info-Type"},
				{goName: "Value", avpName: "User-Equipment-Info-Value"},
			},
		},
		{
			name: "RequestedServiceUnit", avpName: "Requested-Service-Unit",
			doc: "RequestedServiceUnit is the Requested-Service-Unit grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "CCTime", avpName: "CC-Time"},
				{goName: "CCTotalOctets", avpName: "CC-Total-Octets"},
				{goName: "CCInputOctets", avpName: "CC-Input-Octets"},
				{goName: "CCOutputOctets", avpName: "CC-Output-Octets"},
				{goName: "CCServiceSpecificUnits", avpName: "CC-Service-Specific-Units"},
			},
		},
		{
			name: "GrantedServiceUnit", avpName: "Granted-Service-Unit",
			doc: "GrantedServiceUnit is the Granted-Service-Unit grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "CCTime", avpName: "CC-Time"},
				{goName: "CCTotalOctets", avpName: "CC-Total-Octets"},
				{goName: "CCInputOctets", avpName: "CC-Input-Octets"},
				{goName: "CCOutputOctets", avpName: "CC-Output-Octets"},
				{goName: "CCServiceSpecificUnits", avpName: "CC-Service-Specific-Units"},
			},
		},
		{
			name: "UsedServiceUnit", avpName: "Used-Service-Unit",
			doc: "UsedServiceUnit is the Used-Service-Unit grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "TariffChangeUsage", avpName: "Tariff-Change-Usage"},
				{goName: "CCTime", avpName: "CC-Time"},
				{goName: "CCTotalOctets", avpName: "CC-Total-Octets"},
				{goName: "CCInputOctets", avpName: "CC-Input-Octets"},
				{goName: "CCOutputOctets", avpName: "CC-Output-Octets"},
				{goName: "CCServiceSpecificUnits", avpName: "CC-Service-Specific-Units"},
			},
		},
		{
			name: "FinalUnitIndication", avpName: "Final-Unit-Indication",
			doc: "FinalUnitIndication is the Final-Unit-Indication grouped AVP (RFC 4006). Restriction filter rules and redirect servers fall into Other.",
			fields: []field{
				{goName: "FinalUnitAction", avpName: "Final-Unit-Action"},
			},
		},
		{
			name: "Trigger", avpName: "Trigger",
			doc: "Trigger is the 3GPP Trigger grouped AVP (TS 32.299).",
			fields: []field{
				{goName: "TriggerType", avpName: "Trigger-Type", repeated: true},
			},
		},
		{
			name: "PSInformation", avpName: "PS-Information",
			doc: "PSInformation is the 3GPP PS-Information grouped AVP (TS 32.299). Only the OCS-relevant subset is modeled; the rest falls into Other.",
			fields: []field{
				{goName: "TGPPChargingID", avpName: "TGPP-Charging-Id"},
				{goName: "TGPPPDPType", avpName: "TGPP-PDP-Type"},
				{goName: "SGSNAddress", avpName: "SGSN-Address"},
				{goName: "GGSNAddress", avpName: "GGSN-Address"},
				{goName: "CalledStationID", avpName: "Called-Station-Id"},
				{goName: "TGPPRATType", avpName: "TGPP-RAT-Type"},
			},
		},
		{
			name: "ServiceInformation", avpName: "Service-Information",
			doc: "ServiceInformation is the 3GPP Service-Information grouped AVP (TS 32.299).",
			fields: []field{
				{goName: "PSInformation", avpName: "PS-Information", group: "PSInformation"},
			},
		},
		{
			name: "MSCC", avpName: "Multiple-Services-Credit-Control",
			doc: "MSCC is the Multiple-Services-Credit-Control grouped AVP (RFC 4006).",
			fields: []field{
				{goName: "RatingGroup", avpName: "Rating-Group"},
				{goName: "ServiceIdentifier", avpName: "Service-Identifier"},
				{goName: "Requested", avpName: "Requested-Service-Unit", group: "RequestedServiceUnit"},
				{goName: "Used", avpName: "Used-Service-Unit", group: "UsedServiceUnit", repeated: true},
				{goName: "Granted", avpName: "Granted-Service-Unit", group: "GrantedServiceUnit"},
				{goName: "ValidityTime", avpName: "Validity-Time"},
				{goName: "FinalUnitIndication", avpName: "Final-Unit-Indication", group: "FinalUnitIndication"},
				{goName: "Trigger", avpName: "Trigger", group: "Trigger"},
				{goName: "TariffTimeChange", avpName: "Tariff-Time-Change"},
				{goName: "ResultCode", avpName: "Result-Code"},
			},
		},
	},
	messages: []messageDef{
		{
			code: 272, appID: 4,
			structDef: structDef{
				name: "CCR",
				doc:  "CCR is a Gy Credit-Control-Request (Application-Id 4, command 272).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "DestinationRealm", avpName: "Destination-Realm"},
					{goName: "AuthApplicationID", avpName: "Auth-Application-Id"},
					{goName: "ServiceContextID", avpName: "Service-Context-Id"},
					{goName: "CCRequestType", avpName: "CC-Request-Type"},
					{goName: "CCRequestNumber", avpName: "CC-Request-Number"},
					{goName: "DestinationHost", avpName: "Destination-Host"},
					{goName: "SubscriptionID", avpName: "Subscription-Id", group: "SubscriptionID", repeated: true},
					{goName: "UserEquipmentInfo", avpName: "User-Equipment-Info", group: "UserEquipmentInfo"},
					{goName: "TerminationCause", avpName: "Termination-Cause"},
					{goName: "OriginStateID", avpName: "Origin-State-Id"},
					{goName: "EventTimestamp", avpName: "Event-Timestamp"},
					{goName: "MSCC", avpName: "Multiple-Services-Credit-Control", group: "MSCC", repeated: true},
					{goName: "ServiceInformation", avpName: "Service-Information", group: "ServiceInformation"},
				},
			},
		},
		{
			code: 272, appID: 4,
			structDef: structDef{
				name: "CCA",
				doc:  "CCA is a Gy Credit-Control-Answer (Application-Id 4, command 272).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "ResultCode", avpName: "Result-Code"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "AuthApplicationID", avpName: "Auth-Application-Id"},
					{goName: "CCRequestType", avpName: "CC-Request-Type"},
					{goName: "CCRequestNumber", avpName: "CC-Request-Number"},
					{goName: "MSCC", avpName: "Multiple-Services-Credit-Control", group: "MSCC", repeated: true},
					{goName: "CCSessionFailover", avpName: "CC-Session-Failover"},
					{goName: "CreditControlFailureHandling", avpName: "Credit-Control-Failure-Handling"},
				},
			},
		},
	},
}

// ---- Sy (spending-limit reporting, 3GPP TS 29.219) ---------------------

var syApp = application{
	pkg: "sy",
	doc: "Package sy is the generated static codec for Diameter Sy\n" +
		"(Application-Id 16777302: SLR/SLA and SNR/SNA), 3GPP TS 29.219.",
	enums: []enumAVP{
		{"SL-Request-Type", "SLRequestType"},
		{"Subscription-Id-Type", "SubscriptionIDType"},
	},
	groups: []structDef{
		{
			name: "SubscriptionID", avpName: "Subscription-Id",
			doc: "SubscriptionID is the Subscription-Id grouped AVP (RFC 4006, reused by Sy).",
			fields: []field{
				{goName: "Type", avpName: "Subscription-Id-Type"},
				{goName: "Data", avpName: "Subscription-Id-Data"},
			},
		},
		{
			name: "PendingPolicyCounterInformation", avpName: "Pending-Policy-Counter-Information",
			doc: "PendingPolicyCounterInformation is the Pending-Policy-Counter-Information grouped AVP (TS 29.219 §5.3.5).",
			fields: []field{
				{goName: "PolicyCounterStatus", avpName: "Policy-Counter-Status"},
				{goName: "PendingPolicyCounterChangeTime", avpName: "Pending-Policy-Counter-Change-Time"},
			},
		},
		{
			name: "PolicyCounterStatusReport", avpName: "Policy-Counter-Status-Report",
			doc: "PolicyCounterStatusReport is the Policy-Counter-Status-Report grouped AVP (TS 29.219 §5.3.3).",
			fields: []field{
				{goName: "PolicyCounterIdentifier", avpName: "Policy-Counter-Identifier"},
				{goName: "PolicyCounterStatus", avpName: "Policy-Counter-Status"},
				{goName: "PendingPolicyCounterInformation", avpName: "Pending-Policy-Counter-Information", group: "PendingPolicyCounterInformation", repeated: true},
			},
		},
	},
	messages: []messageDef{
		{
			code: 8388635, appID: 16777302,
			structDef: structDef{
				name: "SLR",
				doc:  "SLR is a Sy Spending-Limit-Request (Application-Id 16777302, command 8388635).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "AuthApplicationID", avpName: "Auth-Application-Id"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "DestinationRealm", avpName: "Destination-Realm"},
					{goName: "DestinationHost", avpName: "Destination-Host"},
					{goName: "OriginStateID", avpName: "Origin-State-Id"},
					{goName: "SLRequestType", avpName: "SL-Request-Type"},
					{goName: "SubscriptionID", avpName: "Subscription-Id", group: "SubscriptionID", repeated: true},
					{goName: "PolicyCounterIdentifier", avpName: "Policy-Counter-Identifier", repeated: true},
				},
			},
		},
		{
			code: 8388635, appID: 16777302,
			structDef: structDef{
				name: "SLA",
				doc:  "SLA is a Sy Spending-Limit-Answer (Application-Id 16777302, command 8388635).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "AuthApplicationID", avpName: "Auth-Application-Id"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "ResultCode", avpName: "Result-Code"},
					{goName: "PolicyCounterStatusReport", avpName: "Policy-Counter-Status-Report", group: "PolicyCounterStatusReport", repeated: true},
					{goName: "OriginStateID", avpName: "Origin-State-Id"},
				},
			},
		},
		{
			code: 8388636, appID: 16777302,
			structDef: structDef{
				name: "SNR",
				doc:  "SNR is a Sy Spending-Status-Notification-Request (Application-Id 16777302, command 8388636).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "DestinationRealm", avpName: "Destination-Realm"},
					{goName: "DestinationHost", avpName: "Destination-Host"},
					{goName: "AuthApplicationID", avpName: "Auth-Application-Id"},
					{goName: "OriginStateID", avpName: "Origin-State-Id"},
					{goName: "PolicyCounterStatusReport", avpName: "Policy-Counter-Status-Report", group: "PolicyCounterStatusReport", repeated: true},
					{goName: "SNRequestType", avpName: "SN-Request-Type"},
				},
			},
		},
		{
			code: 8388636, appID: 16777302,
			structDef: structDef{
				name: "SNA",
				doc:  "SNA is a Sy Spending-Status-Notification-Answer (Application-Id 16777302, command 8388636).",
				fields: []field{
					{goName: "SessionID", avpName: "Session-Id"},
					{goName: "OriginHost", avpName: "Origin-Host"},
					{goName: "OriginRealm", avpName: "Origin-Realm"},
					{goName: "ResultCode", avpName: "Result-Code"},
					{goName: "OriginStateID", avpName: "Origin-State-Id"},
				},
			},
		},
	},
}
