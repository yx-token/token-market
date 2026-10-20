// s2a-compat 在升级 sub2api 之前检查目标版本是否与 token-market 冲突。
//
// 用法：
//
//	s2a-compat check --sub2api-repo PATH --target REF (--current REF | --replica-dsn DSN) [--target-version X.Y.Z] [--scratch-dsn DSN] [--format md|json] [--out FILE] [--fail-on conflict|warning]
//	s2a-compat routes --base-url URL [--format md|json] [--out FILE]
//	s2a-compat fingerprint --sub2api-repo PATH --ref REF
//	s2a-compat fingerprint --replica-dsn DSN
//
// 升级流程（docs/06）：check 静态预检（表结构 + 契约版本）→ 预发环境升级后 routes 探测接口 → 通过后再升级生产。
//
// 退出码：0 通过；1 有阻止升级的发现；2 检查本身出错。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/yx-token/idlicat/market/contract"
	"github.com/yx-token/idlicat/market/internal/compat"
	"github.com/yx-token/idlicat/market/internal/sub2api/replica"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	exitOK       = 0
	exitBlocked  = 1
	exitErrored  = 2
	queryTimeout = 30 * time.Second
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitErrored
	}
	var err error
	code := exitOK
	switch args[0] {
	case "check":
		code, err = runCheck(args[1:], stdout)
	case "routes":
		code, err = runRoutes(args[1:], stdout)
	case "fingerprint":
		err = runFingerprint(args[1:], stdout)
	case "-h", "--help", "help":
		usage(stdout)
		return exitOK
	default:
		usage(stderr)
		return exitErrored
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "s2a-compat:", err)
		return exitErrored
	}
	return code
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `s2a-compat: 检查 sub2api 升级是否与 token-market 冲突

  s2a-compat check --sub2api-repo PATH --target REF (--current REF | --replica-dsn DSN) [--target-version X.Y.Z]
                   [--scratch-dsn DSN] [--format md|json] [--out FILE] [--fail-on conflict|warning] [--contract FILE]
  s2a-compat routes --base-url URL [--format md|json] [--out FILE] [--contract FILE]
  s2a-compat fingerprint (--sub2api-repo PATH --ref REF | --replica-dsn DSN)
`)
}

func runCheck(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	repo := fs.String("sub2api-repo", "", "sub2api 本地 git 仓库路径")
	target := fs.String("target", "", "目标版本的 git ref（如 v0.2.15）")
	targetDir := fs.String("target-dir", "", "目标版本迁移目录（代替 --sub2api-repo/--target）")
	current := fs.String("current", "", "当前版本的 git ref（不读生产库时使用）")
	replicaDSN := fs.String("replica-dsn", "", "sub2api 只读库 DSN，读取实际已执行的迁移")
	format := fs.String("format", "md", "报告格式：md 或 json")
	out := fs.String("out", "", "报告输出文件，默认标准输出")
	failOn := fs.String("fail-on", string(compat.SeverityConflict), "达到该级别即视为阻止升级：conflict 或 warning")
	contractFile := fs.String("contract", "", "覆盖内嵌契约的 contract.yaml 路径")
	scratchDSN := fs.String("scratch-dsn", "", "空的临时库 DSN：执行目标版本迁移后对契约查询做 EXPLAIN")
	targetVersion := fs.String("target-version", "", "目标版本号（如 0.2.15）：同时检查本系统契约是否已登记该版本，未登记则阻止升级")
	if err := fs.Parse(args); err != nil {
		return exitErrored, err
	}

	threshold := compat.Severity(*failOn)
	if threshold != compat.SeverityConflict && threshold != compat.SeverityWarning {
		return exitErrored, fmt.Errorf("--fail-on must be conflict or warning")
	}
	if *format != "md" && *format != "json" {
		return exitErrored, fmt.Errorf("--format must be md or json")
	}

	spec, err := loadSpec(*contractFile)
	if err != nil {
		return exitErrored, err
	}

	ctx := context.Background()
	var targetSet *compat.MigrationSet
	switch {
	case *targetDir != "":
		targetSet, err = compat.LoadDirMigrations(*targetDir)
	case *repo != "" && *target != "":
		targetSet, err = compat.LoadGitMigrations(ctx, *repo, *target)
	default:
		return exitErrored, errors.New("need --target-dir, or --sub2api-repo with --target")
	}
	if err != nil {
		return exitErrored, err
	}

	var applied compat.Applied
	switch {
	case *replicaDSN != "":
		applied, err = appliedFromReplica(ctx, *replicaDSN)
	case *current != "" && *repo != "":
		var set *compat.MigrationSet
		set, err = compat.LoadGitMigrations(ctx, *repo, *current)
		if err == nil {
			applied = compat.AppliedFromSet(set)
		}
	default:
		return exitErrored, errors.New("need --replica-dsn, or --sub2api-repo with --current")
	}
	if err != nil {
		return exitErrored, err
	}

	report := compat.CheckStatic(ctx, spec, applied, targetSet)
	if *targetVersion != "" {
		report.Findings = append(compat.CheckContractVersion(spec, *targetVersion, applied, targetSet), report.Findings...)
	}
	if *scratchDSN != "" {
		findings, err := compat.CheckQueries(ctx, *scratchDSN, targetSet)
		if err != nil {
			return exitErrored, fmt.Errorf("query check: %w", err)
		}
		report.Findings = append(findings, report.Findings...)
	}
	var rendered []byte
	if *format == "json" {
		if rendered, err = report.JSON(); err != nil {
			return exitErrored, err
		}
	} else {
		rendered = []byte(report.Markdown())
	}
	if *out != "" {
		if err := os.WriteFile(*out, rendered, 0o644); err != nil {
			return exitErrored, err
		}
	} else if _, err := stdout.Write(rendered); err != nil {
		return exitErrored, err
	}

	if report.Failed(threshold) {
		return exitBlocked, nil
	}
	return exitOK, nil
}

func runRoutes(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	baseURL := fs.String("base-url", "", "待验证的 sub2api 地址（预发环境）")
	format := fs.String("format", "md", "报告格式：md 或 json")
	out := fs.String("out", "", "报告输出文件，默认标准输出")
	contractFile := fs.String("contract", "", "覆盖内嵌契约的 contract.yaml 路径")
	if err := fs.Parse(args); err != nil {
		return exitErrored, err
	}
	if *baseURL == "" {
		return exitErrored, errors.New("--base-url is required")
	}
	spec, err := loadSpec(*contractFile)
	if err != nil {
		return exitErrored, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, findings, err := compat.ProbeRoutes(ctx, *baseURL, spec, nil)
	if err != nil {
		return exitErrored, err
	}
	var rendered []byte
	switch *format {
	case "json":
		rendered, err = json.MarshalIndent(map[string]any{"results": results, "findings": findings}, "", "  ")
		if err != nil {
			return exitErrored, err
		}
	case "md":
		rendered = []byte(compat.RoutesMarkdown(results, findings))
	default:
		return exitErrored, errors.New("--format must be md or json")
	}
	if *out != "" {
		err = os.WriteFile(*out, rendered, 0o644)
	} else {
		_, err = stdout.Write(rendered)
	}
	if err != nil {
		return exitErrored, err
	}
	if len(findings) > 0 {
		return exitBlocked, nil
	}
	return exitOK, nil
}

func runFingerprint(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fingerprint", flag.ContinueOnError)
	repo := fs.String("sub2api-repo", "", "sub2api 本地 git 仓库路径")
	ref := fs.String("ref", "", "git ref")
	replicaDSN := fs.String("replica-dsn", "", "sub2api 只读库 DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	var names []string
	switch {
	case *replicaDSN != "":
		applied, err := appliedFromReplica(ctx, *replicaDSN)
		if err != nil {
			return err
		}
		names = applied.Names()
	case *repo != "" && *ref != "":
		set, err := compat.LoadGitMigrations(ctx, *repo, *ref)
		if err != nil {
			return err
		}
		names = set.Names
	default:
		return errors.New("need --replica-dsn, or --sub2api-repo with --ref")
	}
	latest := ""
	if len(names) > 0 {
		latest = names[len(names)-1]
	}
	_, err := fmt.Fprintf(stdout, "fingerprint: %s\nmigrations:  %d\nlatest:      %s\n", contract.MigrationFingerprint(names), len(names), latest)
	return err
}

func loadSpec(path string) (*contract.Spec, error) {
	if path == "" {
		return contract.Load()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return contract.Parse(raw)
}

func appliedFromReplica(ctx context.Context, dsn string) (compat.Applied, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return compat.Applied{}, err
	}
	defer pool.Close()
	r, err := replica.New(pool)
	if err != nil {
		return compat.Applied{}, err
	}
	rows, err := r.AppliedMigrations(ctx)
	if err != nil {
		return compat.Applied{}, err
	}
	applied := compat.Applied{Desc: "replica schema_migrations", Checksums: make(map[string]string, len(rows))}
	for _, m := range rows {
		applied.Checksums[m.Filename] = m.Checksum
	}
	return applied, nil
}
