from pathlib import Path


path = Path("tgws/proxy/tg_ws_proxy.py")
source = path.read_text(encoding="utf-8")
replacements = {
    "    if args.secret:\n        secret_hex = args.secret.strip()\n":
        "    secret_arg = args.secret or os.environ.pop('PRIVET_TG_SECRET', None)\n"
        "    if secret_arg:\n        secret_hex = secret_arg.strip()\n",
    '    log.info("  Secret:        %s", proxy_config.secret)\n':
        '    log.info("  Secret:        managed by PrivetZapret")\n',
    '        log.info("    %s", ee_link)\n':
        '        log.info("    connect through PrivetZapret")\n',
    '        log.info("    %s", dd_link)\n':
        '        log.info("    connect through PrivetZapret")\n',
}
for old, new in replacements.items():
    if source.count(old) != 1:
        raise SystemExit(f"pinned TG WS source changed near: {old!r}")
    source = source.replace(old, new)
path.write_text(source, encoding="utf-8")
