-- Ref: .teos/plans/2026-11-26-side-panels-design.md — profile shell
-- (login-shell wrap / shell-only spawn) + named launch variants.
ALTER TABLE launch_profiles ADD COLUMN shell    TEXT NOT NULL DEFAULT '';
ALTER TABLE launch_profiles ADD COLUMN variants TEXT NOT NULL DEFAULT '[]';
