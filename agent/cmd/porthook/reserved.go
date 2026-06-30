// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"
)

const reservedUsageText = `usage: porthook reserved create --control-plane URL --name NAME --token-id TOKEN_ID [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook reserved list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]
       porthook reserved delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_NAME [--json]
       porthook reserved help`

type reservationCreateConfig struct {
	tokenAdminConfig
	name    string
	tokenID string
}

type reservationDeleteConfig struct {
	tokenAdminConfig
	target string
}

type createReservationRequest struct {
	Name    string `json:"name"`
	TokenID string `json:"token_id"`
}

type createdReservation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenID   string    `json:"token_id"`
	CreatedAt time.Time `json:"created_at"`
}

type reservationSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenID   string    `json:"token_id"`
	CreatedAt time.Time `json:"created_at"`
}

type listReservationsResponse struct {
	ReservedSubdomains []reservationSummary `json:"reserved_subdomains"`
}

func runReservedCommand(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return reservedUsageError()
	}

	switch args[0] {
	case "create":
		if wantsHelp(args[1:]) {
			printReservationCreateHelp(stdout)
			return nil
		}
		return runReservedCreate(args[1:], stdin, stdout, stderr)
	case "list":
		if wantsHelp(args[1:]) {
			printReservationListHelp(stdout)
			return nil
		}
		return runReservedList(args[1:], stdin, stdout, stderr)
	case "delete":
		if wantsHelp(args[1:]) {
			printReservationDeleteHelp(stdout)
			return nil
		}
		return runReservedDelete(args[1:], stdin, stdout, stderr)
	case "help", "--help", "-h":
		printReservedUsage(stdout)
		return nil
	default:
		return reservedUsageError()
	}
}

func runReservedCreate(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseReservationCreateConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	created, err := client.createReservation(context.Background(), createReservationRequest{
		Name:    cfg.name,
		TokenID: cfg.tokenID,
	})
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, created)
	}
	printCreatedReservation(stdout, created)
	return nil
}

func runReservedList(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseReservationListConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg)
	listed, err := client.listReservations(context.Background())
	if err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, listed)
	}
	printReservationList(stdout, listed.ReservedSubdomains)
	return nil
}

func runReservedDelete(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cfg, err := parseReservationDeleteConfig(args, stdin, stderr)
	if err != nil {
		return err
	}

	client := newTokenAdminClient(cfg.tokenAdminConfig)
	deleted, err := client.resolveReservationDeleteTarget(context.Background(), cfg.target)
	if err != nil {
		return err
	}
	if err := client.deleteReservation(context.Background(), deleted.ID); err != nil {
		return err
	}
	if cfg.jsonOutput {
		return writeJSONOutput(stdout, map[string]any{
			"id":      deleted.ID,
			"name":    deleted.Name,
			"deleted": true,
		})
	}
	if deleted.Name != "" {
		fmt.Fprintf(stdout, "Reserved subdomain deleted: %s (%s)\n", deleted.Name, deleted.ID)
		return nil
	}
	fmt.Fprintf(stdout, "Reserved subdomain deleted: %s\n", deleted.ID)
	return nil
}

func parseReservationCreateConfig(args []string, stdin io.Reader, stderr io.Writer) (reservationCreateConfig, error) {
	var cfg reservationCreateConfig
	fs := newTokenAdminFlagSet("reserved create", &cfg.tokenAdminConfig, stderr)
	fs.StringVar(&cfg.name, "name", "", "reserved subdomain name")
	fs.StringVar(&cfg.tokenID, "token-id", "", "agent token ID that owns the reserved subdomain")
	if err := fs.Parse(args); err != nil {
		return reservationCreateConfig{}, err
	}
	if fs.NArg() > 0 {
		return reservationCreateConfig{}, fmt.Errorf("usage: porthook reserved create --control-plane URL --name NAME --token-id TOKEN_ID [--admin-token TOKEN | --admin-token-stdin]")
	}
	cfg.name = strings.TrimSpace(cfg.name)
	if cfg.name == "" {
		return reservationCreateConfig{}, fmt.Errorf("reserved subdomain name is required")
	}
	cfg.tokenID = strings.TrimSpace(cfg.tokenID)
	if cfg.tokenID == "" {
		return reservationCreateConfig{}, fmt.Errorf("token ID is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return reservationCreateConfig{}, err
	}
	return cfg, nil
}

func parseReservationListConfig(args []string, stdin io.Reader, stderr io.Writer) (tokenAdminConfig, error) {
	var cfg tokenAdminConfig
	fs := newTokenAdminFlagSet("reserved list", &cfg, stderr)
	if err := fs.Parse(args); err != nil {
		return tokenAdminConfig{}, err
	}
	if fs.NArg() > 0 {
		return tokenAdminConfig{}, fmt.Errorf("usage: porthook reserved list --control-plane URL [--admin-token TOKEN | --admin-token-stdin]")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg, stdin, stderr); err != nil {
		return tokenAdminConfig{}, err
	}
	return cfg, nil
}

func parseReservationDeleteConfig(args []string, stdin io.Reader, stderr io.Writer) (reservationDeleteConfig, error) {
	var cfg reservationDeleteConfig
	fs := newTokenAdminFlagSet("reserved delete", &cfg.tokenAdminConfig, stderr)
	if err := fs.Parse(args); err != nil {
		return reservationDeleteConfig{}, err
	}
	if fs.NArg() != 1 {
		return reservationDeleteConfig{}, fmt.Errorf("usage: porthook reserved delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_NAME")
	}
	cfg.target = strings.TrimSpace(fs.Arg(0))
	if cfg.target == "" {
		return reservationDeleteConfig{}, fmt.Errorf("reserved subdomain id or name is required")
	}
	if err := finalizeTokenAdminConfig(fs, &cfg.tokenAdminConfig, stdin, stderr); err != nil {
		return reservationDeleteConfig{}, err
	}
	return cfg, nil
}

func (c tokenAdminClient) createReservation(ctx context.Context, req createReservationRequest) (createdReservation, error) {
	var created createdReservation
	if err := c.do(ctx, http.MethodPost, "/api/v1/reserved-subdomains", req, &created); err != nil {
		return createdReservation{}, err
	}
	return created, nil
}

func (c tokenAdminClient) listReservations(ctx context.Context) (listReservationsResponse, error) {
	var listed listReservationsResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/reserved-subdomains", nil, &listed); err != nil {
		return listReservationsResponse{}, err
	}
	return listed, nil
}

func (c tokenAdminClient) deleteReservation(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/reserved-subdomains/"+url.PathEscape(id), nil, nil)
}

func (c tokenAdminClient) resolveReservationDeleteTarget(ctx context.Context, target string) (reservationSummary, error) {
	if strings.HasPrefix(target, "rs_") {
		return reservationSummary{ID: target}, nil
	}

	listed, err := c.listReservations(ctx)
	if err != nil {
		return reservationSummary{}, err
	}
	name := strings.ToLower(strings.TrimSpace(target))
	for _, reservation := range listed.ReservedSubdomains {
		if reservation.Name == name {
			return reservation, nil
		}
	}
	return reservationSummary{}, fmt.Errorf("reserved subdomain %q was not found", target)
}

func printCreatedReservation(w io.Writer, reservation createdReservation) {
	fmt.Fprintf(w, "Created reserved subdomain %s\n", reservation.ID)
	fmt.Fprintf(w, "Name: %s\n", reservation.Name)
	fmt.Fprintf(w, "Token ID: %s\n", reservation.TokenID)
	fmt.Fprintf(w, "Created: %s\n", reservation.CreatedAt.Format(time.RFC3339))
}

func printReservationList(w io.Writer, reservations []reservationSummary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTOKEN ID\tCREATED")
	for _, reservation := range reservations {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\n",
			reservation.ID,
			reservation.Name,
			reservation.TokenID,
			reservation.CreatedAt.Format(time.RFC3339),
		)
	}
	_ = tw.Flush()
}

func reservedUsageError() error {
	return fmt.Errorf("%s", reservedUsageText)
}

func printReservedUsage(w io.Writer) {
	fmt.Fprintln(w, reservedUsageText)
}

func printReservationCreateHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook reserved create --control-plane URL --name NAME --token-id TOKEN_ID [--admin-token TOKEN | --admin-token-stdin] [--json]

Reserve a requested public subdomain for one agent token.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --name NAME               Reserved subdomain name.
  --token-id TOKEN_ID       Agent token ID that owns the reserved subdomain.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.`)
}

func printReservationListHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook reserved list --control-plane URL [--admin-token TOKEN | --admin-token-stdin] [--json]

List reserved subdomains.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.`)
}

func printReservationDeleteHelp(w io.Writer) {
	fmt.Fprintln(w, `usage: porthook reserved delete --control-plane URL [--admin-token TOKEN | --admin-token-stdin] ID_OR_NAME [--json]

Delete a reserved subdomain by reservation ID or subdomain name.

Options:
  --control-plane URL       Control-plane API URL. Defaults to PORTHOOK_CONTROL_PLANE_URL.
  --admin-token TOKEN       Control-plane admin token. Prefer --admin-token-stdin outside local development.
  --admin-token-stdin       Read the control-plane admin token from stdin.
  --json                    Write JSON output.`)
}
