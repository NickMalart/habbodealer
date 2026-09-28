import os
import re

def migrate_file(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    original_content = content

    # 1. Imports
    content = content.replace('"github.com/jackc/pgx/v5/pgxpool"', '"database/sql"\n\t_ "modernc.org/sqlite"')
    content = content.replace('"github.com/jackc/pgx/v5"', '')

    # 2. pgxpool.Pool -> sql.DB
    content = content.replace('*pgxpool.Pool', '*sql.DB')
    
    # 3. Context methods
    content = re.sub(r'\b(db|pool|tx)\.Exec\(ctx,', r'\1.ExecContext(ctx,', content)
    content = re.sub(r'\b(db|pool|tx)\.Query\(ctx,', r'\1.QueryContext(ctx,', content)
    content = re.sub(r'\b(db|pool|tx)\.QueryRow\(ctx,', r'\1.QueryRowContext(ctx,', content)
    
    # 4. Connection setup
    # Look for pgxpool.NewWithConfig or pgxpool.New
    conn_pattern1 = r'cfg,\s*err\s*:=\s*pgxpool\.ParseConfig\(([^)]+)\)\s*if\s*err\s*!=\s*nil\s*\{[^}]+\}\s*(pool|db),\s*err\s*:=\s*pgxpool\.NewWithConfig\([^,]+,\s*cfg\)'
    def conn_repl1(m):
        conn_var = m.group(1)
        db_var = m.group(2)
        return f"{conn_var} = strings.TrimPrefix({conn_var}, \"file:\")\n\t{db_var}, err := sql.Open(\"sqlite\", {conn_var})"
    content = re.sub(conn_pattern1, conn_repl1, content, flags=re.DOTALL)
    
    conn_pattern2 = r'(pool|db),\s*err\s*:=\s*pgxpool\.New\([^,]+,\s*([^)]+)\)'
    def conn_repl2(m):
        db_var = m.group(1)
        conn_var = m.group(2)
        return f"{conn_var} = strings.TrimPrefix({conn_var}, \"file:\")\n\t{db_var}, err := sql.Open(\"sqlite\", {conn_var})"
    content = re.sub(conn_pattern2, conn_repl2, content)

    # 5. SQL parameters ($1 -> ?)
    # We will just replace \$(\d+) with ? globally in string literals.
    # A simple regex for string literals and backticks.
    def sql_repl(m):
        text = m.group(0)
        # only replace if it's inside a string (but simpler to just replace all $1 globally since $ is rare in Go except for template strings or env vars, but mostly in queries)
        return re.sub(r'\$\d+', '?', text)
    
    content = re.sub(r'`[^`]*`', sql_repl, content)
    content = re.sub(r'"(?:\\.|[^"\\])*"', sql_repl, content)

    # 6. Ping
    content = content.replace('.Ping(ctx)', '.PingContext(ctx)')
    
    # 7. Rows loop
    # In database/sql, rows.Scan requires sql.NullString etc., but standard variables also work if no nulls.
    # pgx uses specific err != nil check for tx.Commit.
    
    # 8. plpgsql syntax (basic replacements)
    content = content.replace('BIGSERIAL PRIMARY KEY', 'INTEGER PRIMARY KEY AUTOINCREMENT')
    content = content.replace('TIMESTAMPTZ', 'DATETIME')
    content = content.replace('NOW()', 'CURRENT_TIMESTAMP')
    content = content.replace('::jsonb', '')
    content = content.replace('JSONB', 'TEXT')
    content = content.replace('to_timestamp(trim(e.started_at), \'YYYY-MM-DD"T"HH24:MI:SS\') AT TIME ZONE \'UTC\'', 'datetime(e.started_at)')
    content = content.replace('$$ LANGUAGE plpgsql', '')
    content = content.replace('ILIKE', 'LIKE')
    content = content.replace('public.', '')

    # Write back if changed
    if content != original_content:
        with open(filepath, 'w', encoding='utf-8') as f:
            f.write(content)
        print(f"Migrated {filepath}")

def find_and_migrate(directory):
    for root, dirs, files in os.walk(directory):
        if '.git' in root or 'node_modules' in root or 'frontend' in root:
            continue
        for file in files:
            if file.endswith('.go'):
                migrate_file(os.path.join(root, file))

if __name__ == '__main__':
    find_and_migrate('.')
