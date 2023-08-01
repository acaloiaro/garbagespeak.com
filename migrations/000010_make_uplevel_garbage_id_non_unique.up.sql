DROP INDEX IF EXISTS garbage_id_idx;
CREATE INDEX IF NOT EXISTS uplevel_garbage_id_idx ON public.uplevels USING btree (garbage_id);

