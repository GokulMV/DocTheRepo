-- A search piece written from several repositories (the System architecture) carries all of them: it is
-- found only by someone who can read every one. NULL: the piece's own repository (or shared source) decides.
ALTER TABLE chunks ADD COLUMN requires_repos uuid[];
