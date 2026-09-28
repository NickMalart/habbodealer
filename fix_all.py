import re, os, glob

files = (
    glob.glob('scripts/**/*.go', recursive=True) +
    glob.glob('tools/**/*.go', recursive=True) +
    ['main.go', 'casino_stats.go', 'app.go']
)

for fname in files:
    if not os.path.exists(fname):
        continue
    try:
        with open(fname, 'r', encoding='utf-8') as f:
            c = f.read()
        orig = c

        # Remove duplicate database/sql imports
        c = re.sub(r'(\t"database/sql"\n)(\t"database/sql"\n)', r'\1', c)

        # Fix pgx.Rows -> *sql.Rows
        c = c.replace('pgx.Rows', '*sql.Rows')

        # Fix Begin(ctx) -> BeginTx(ctx, nil)
        c = re.sub(r'\.Begin\((\w+)\)', lambda m: '.BeginTx(' + m.group(1) + ', nil)', c)

        # Fix Rollback(ctx)/Commit(ctx) -> Rollback()/Commit()
        c = re.sub(r'tx\.Rollback\(\w+\)', 'tx.Rollback()', c)
        c = re.sub(r'tx\.Commit\(\w+\)', 'tx.Commit()', c)

        # Fix inline context API calls
        for ctx_var in ['ctx', 'ctxP', 'ctx2', 'ctx3']:
            c = c.replace('.QueryRow(' + ctx_var + ',', '.QueryRowContext(' + ctx_var + ',')
            c = c.replace('.Query(' + ctx_var + ',', '.QueryContext(' + ctx_var + ',')
            c = c.replace('.Exec(' + ctx_var + ',', '.ExecContext(' + ctx_var + ',')
        c = c.replace('.QueryRow(context.Background(),', '.QueryRowContext(context.Background(),')
        c = c.replace('.Query(context.Background(),', '.QueryContext(context.Background(),')
        c = c.replace('.Exec(context.Background(),', '.ExecContext(context.Background(),')

        # Fix single-value RowsAffected()
        c = re.sub(r'(\w+)\s*:=\s*(\w+)\.RowsAffected\(\)', r'\1, _ := \2.RowsAffected()', c)

        if c != orig:
            with open(fname, 'w', encoding='utf-8') as f:
                f.write(c)
            print('Fixed ' + fname)
    except Exception as e:
        print('Err ' + fname + ': ' + str(e))

print('Done')
