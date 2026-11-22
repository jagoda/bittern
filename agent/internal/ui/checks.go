package ui

import (
	"context"
	"fmt"
	"strings"

	"bittern.invalid/agent/internal/status"
)

// AgentChecks maps the read-only status document onto the setup checks. It is
// the production implementation. MockChecks is selected only for previews and
// tests.
type AgentChecks struct {
	Builder *status.Builder
}

// Network returns the lan0 and cam0 address and carrier state from the same
// status builder that serves the status page.
func (c AgentChecks) Network(ctx context.Context) NetworkCheck {
	return networkCheck(c.document(ctx))
}

// Storage returns the recording media state from the same status builder that
// serves the status page.
func (c AgentChecks) Storage(ctx context.Context) StorageCheck {
	return storageCheck(c.document(ctx))
}

func (c AgentChecks) document(ctx context.Context) status.Document {
	return BuilderStatusProvider{Builder: c.Builder}.Status(ctx, "")
}

func networkCheck(document status.Document) NetworkCheck {
	section := document.Sections.Network
	check := NetworkCheck{
		LANAddress: interfaceAddress(section.LAN0),
		LANCarrier: interfaceCarrier(section.LAN0),
		CAMAddress: interfaceAddress(section.CAM0),
		CAMCarrier: interfaceCarrier(section.CAM0),
	}
	if section.State != status.SectionOK && section.State != status.SectionDegraded {
		check.Detail = "Bittern could not check the network. Open status for the reason, then try again."
		return check
	}
	check.Detail = "House network: " + connectionDetail(check.LANAddress, check.LANCarrier) +
		" Camera network: " + connectionDetail(check.CAMAddress, check.CAMCarrier)
	if section.State == status.SectionDegraded {
		check.Detail = "The network check found a problem. " + check.Detail
	}
	return check
}

func connectionDetail(address string, carrier bool) string {
	state := "cable disconnected"
	if carrier {
		state = "cable connected"
	}
	return "address " + address + ", " + state + "."
}

func storageCheck(document status.Document) StorageCheck {
	section := document.Sections.Storage
	check := StorageCheck{
		Path:    storagePath(section.Path),
		Present: section.Present,
		Free:    storageAmount(section.FreeBytes, "Unavailable"),
		Reserve: storageAmount(section.ReserveBytes, "Unknown"),
	}
	if section.State == status.SectionUnknown {
		check.Detail = "Bittern could not check the recording drive. Open status for the reason, then try again."
		return check
	}

	details := make([]string, 0, 4)
	switch section.DetailState {
	case status.StorageDetailNotAttached:
		details = append(details, "The recording drive is not connected. Recording is stopped. Connect the drive, then check again.")
	case status.StorageDetailNotMounted:
		details = append(details, "The recording drive is connected but Bittern cannot write to it. Recording is stopped. Check the drive, then run the storage setup again.")
	case status.StorageDetailWriteTestFailed:
		details = append(details, "The recording drive is connected but Bittern could not write to it. Recording fails until the drive is repaired or replaced.")
	case status.StorageDetailReady:
		details = append(details, "The recording drive is separate from the one the appliance runs from and has room for new recordings.")
	default:
		details = append(details, "Bittern could not check the recording drive. Open status for the reason, then try again.")
	}
	if section.MemberCount != nil {
		details = append(details, memberCountDetail(*section.MemberCount))
	}
	if len(section.MissingMembers) > 0 {
		details = append(details, "Missing drives: "+strings.Join(section.MissingMembers, ", ")+".")
	} else if section.MemberCount != nil {
		details = append(details, "No drives are missing.")
	}
	if section.State == status.SectionDegraded {
		details = append([]string{"The recording drive check found a problem."}, details...)
	}
	check.Detail = strings.Join(details, " ")
	return check
}

func interfaceAddress(section status.InterfaceSection) string {
	if section.Address == nil || strings.TrimSpace(*section.Address) == "" {
		return "Unavailable"
	}
	return *section.Address
}

func interfaceCarrier(section status.InterfaceSection) bool {
	return section.Carrier != nil && *section.Carrier
}

func storagePath(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unavailable"
	}
	return value
}

func storageAmount(value *uint64, fallback string) string {
	if value == nil {
		return fallback
	}
	return humanBytes(*value)
}

func memberCountDetail(count int) string {
	if count == 1 {
		return "Bittern sees one recording drive."
	}
	return fmt.Sprintf("Bittern sees %d recording drives.", count)
}
