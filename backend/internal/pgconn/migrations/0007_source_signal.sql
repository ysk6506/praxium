-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0007: the mark that a receiving webhook of kind signal came for a source (ADR-0107).
--
-- A signal says "the source changed, read it again" and carries nothing, so the whole of it is one time: when the first
-- signal that has not been taken yet came. It is set only when it is empty (a call that comes again changes no row), and
-- what takes it (the opening of a run, a later change) clears it. This is an expand step: it only adds, and a row that
-- has none is a source with no signal waiting.
ALTER TABLE source_state ADD COLUMN signal_pending_at timestamptz;
