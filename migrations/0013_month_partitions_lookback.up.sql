-- Keep the previous month's partition too: a row stamped just before midnight on the last day of a month
-- (a delayed ledger write, clock skew, a backfill) must not fail with "no partition found".
CREATE OR REPLACE FUNCTION ensure_month_partitions(tbl text, months_ahead integer) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    m     date := date_trunc('month', now())::date;
    i     integer;
    lo    date;
    hi    date;
    pname text;
BEGIN
    FOR i IN -1..months_ahead LOOP
        lo := (m + make_interval(months => i))::date;
        hi := (m + make_interval(months => i + 1))::date;
        pname := format('%s_%s', tbl, to_char(lo, 'YYYYMM'));
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                       pname, tbl, lo, hi);
    END LOOP;
END $$;

SELECT ensure_month_partitions('usage_events', 3);
