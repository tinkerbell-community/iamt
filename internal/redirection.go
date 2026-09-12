// SPDX-License-Identifier: MPL-2.0

package internal

import (
	"context"
	"fmt"

	"github.com/device-management-toolkit/go-wsman-messages/v2/pkg/wsman/amt/redirection"
)

// EnableRedirection turns on the AMT redirection service (IDER and SOL) and
// enables its network listener, so the redirection ports 16994/16995 accept
// connections. It is idempotent: a device already in this state is left
// unchanged.
//
// Both steps are required. RequestStateChange sets the administrative state,
// but the listener only accepts connections once ListenerEnabled is also true.
func (c *Client) EnableRedirection(_ context.Context) error {
	if _, err := c.Msg.AMT.RedirectionService.RequestStateChange(redirection.EnableIDERAndSOL); err != nil {
		return fmt.Errorf("iamt: enabling redirection service: %w", err)
	}

	cur, err := c.Msg.AMT.RedirectionService.Get()
	if err != nil {
		return fmt.Errorf("iamt: reading redirection service: %w", err)
	}
	resp := cur.Body.GetAndPutResponse
	if resp.ListenerEnabled && resp.EnabledState == redirection.IDERAndSOLAreEnabled {
		return nil
	}

	req := redirection.RedirectionRequest{
		CreationClassName:       resp.CreationClassName,
		ElementName:             resp.ElementName,
		EnabledState:            redirection.IDERAndSOLAreEnabled,
		ListenerEnabled:         true,
		Name:                    resp.Name,
		SystemCreationClassName: resp.SystemCreationClassName,
		SystemName:              resp.SystemName,
	}
	if _, err := c.Msg.AMT.RedirectionService.Put(req); err != nil {
		return fmt.Errorf("iamt: enabling redirection listener: %w", err)
	}
	return nil
}
