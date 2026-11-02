#!/usr/bin/env python3
# check-sql-coverage.py — 「上层有值、底层丢字段」类 bug 的 CI 闸门
# 教训：accounts.source 与 plans 日配额列被 repository INSERT/白名单静默丢弃，
#       人眼静态阅读不可见，只有 schema×SQL 机器比对能稳定拦截。
# 用法：python3 scripts/check-sql-coverage.py [--strict]
#   默认：INSERT 覆盖扫描有缺失只警告（历史遗留需分期处理）；
#   --strict：任何"schema 有列但 INSERT 从不覆盖且无 DEFAULT"直接退出 1（CI 拦截）。
import re, sys, pathlib, glob

ROOT = pathlib.Path(__file__).resolve().parent.parent

def schema_columns():
    cols, defaults = {}, {}
    for f in sorted(glob.glob(str(ROOT / "migrations/*.sql"))):
        s = pathlib.Path(f).read_text()
        for m in re.finditer(r"CREATE TABLE IF NOT EXISTS (\w+)\s*\((.*?)\n\)", s, re.S):
            t, body = m.group(1), m.group(2)
            for line in body.split("\n"):
                line = line.strip().rstrip(",")
                cm = re.match(r"^(\w+)\s+(.*)", line)
                if not cm: continue
                c, rest = cm.group(1), cm.group(2)
                if c.upper() in ("PRIMARY", "FOREIGN", "UNIQUE", "CONSTRAINT", "CHECK"): continue
                cols.setdefault(t, set()).add(c)
                if re.search(r"DEFAULT|GENERATED|SERIAL|IDENTITY", rest, re.I) or c == "created_at":
                    defaults.setdefault(t, set()).add(c)
        for m in re.finditer(r"ALTER TABLE (\w+)\s+ADD COLUMN(?: IF NOT EXISTS)? (\w+)([^\n]*)", s):
            cols.setdefault(m.group(1), set()).add(m.group(2))
            if re.search(r"DEFAULT|GENERATED", m.group(3), re.I):
                defaults.setdefault(m.group(1), set()).add(m.group(2))
    return cols, defaults

def insert_coverage():
    ins = {}
    for f in glob.glob(str(ROOT / "internal/**/*.go"), recursive=True):
        s = pathlib.Path(f).read_text()
        for m in re.finditer(r"INSERT INTO (\w+)\s*\(([^)]*)\)", s):
            cl = set(c.strip().strip('`') for c in m.group(2).split(",") if c.strip())
            ins.setdefault(m.group(1), set()).update(cl)
    return ins

def scan_pairs():
    """同函数内 SELECT 列数 vs Scan 参数数粗配对（排除 COUNT/EXISTS）。"""
    problems = []
    for f in glob.glob(str(ROOT / "internal/repository/*.go")):
        s = pathlib.Path(f).read_text()
        for fm in re.finditer(r"func \([^)]+\) \w+\([^)]*\)[^{]*\{(.*?)\n\}", s, re.S):
            body = fm.group(1)
            def cnt(x):
                n, d = 1, 0
                for ch in x:
                    if ch == '(': d += 1
                    elif ch == ')': d -= 1
                    elif ch == ',' and d == 0: n += 1
                return n
            scans = [cnt(x) for x in re.findall(r"\.Scan\((.*?)\)", body, re.S)]
            for sel in re.findall(r"SELECT\s+(.+?)\s+FROM\s", body, re.S):
                sel_clean = re.sub(r"COUNT\s*\(|EXISTS\s*\(", "(", sel, flags=re.I)
                c = cnt(sel_clean)
                if c <= 1: continue            # COUNT(*)/单列
                if any(abs(c - sc) == 0 for sc in scans): continue
                if len(scans) == 1 and c != scans[0]:
                    problems.append(f"{pathlib.Path(f).name}: SELECT {c} 列 vs Scan {scans[0]} 参")
    return problems

def main():
    strict = "--strict" in sys.argv
    cols, defaults = schema_columns()
    ins = insert_coverage()
    rc = 0

    print("== INSERT 覆盖扫描 ==")
    for t in sorted(cols):
        if t == "goose_db_version": continue
        uncovered = cols[t] - ins.get(t, set()) - defaults.get(t, set()) - {"updated_at"}
        # 常见豁免：仅 UPDATE 写入的运行态列
        runtime_only = {"session_data", "session_data_new", "blocked_until", "last_online",
                        "locked_at", "worker_id", "completed_at", "started_at", "finished_at",
                        "revoked_at", "checked_at", "last_login_at", "deleted_at"}
        # 已人工核实为 UPDATE 写入或死表的列（2026-09-21 审计结论）
        verified_ok = {
            "account_groups": {"name", "tenant_id"},
            "activation_codes": {"used_at", "used_by"},
            "campaign_targets": {"result"},
            "groups": {"last_scraped_at"},
            "scraped_links": {"resolved_at"},
            "support_config": {"qr_code_url"},
            "task_logs": {"target_name"},
            "tenant_subscriptions": {"expires_at", "plan_id", "tenant_id"},  # 死表，待产品决策
        }
        uncovered -= verified_ok.get(t, set())
        uncovered -= runtime_only
        if uncovered:
            level = "ERROR" if strict else "WARN "
            print(f"  [{level}] {t}: INSERT 未覆盖列 {sorted(uncovered)}")
            if strict: rc = 1

    print("== SELECT/Scan 计数扫描 ==")
    for p in scan_pairs():
        print(f"  [WARN ] {p}")

    print(f"\n{'严格模式：发现问题，CI 失败' if rc else '扫描完成（WARN 项建议人工确认）'}")
    sys.exit(rc)

if __name__ == "__main__":
    main()
