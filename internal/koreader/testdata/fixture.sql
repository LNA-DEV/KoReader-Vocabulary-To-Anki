-- Regenerate the reader test fixture:
--   rm -f vocab_fixture.sqlite3 && sqlite3 vocab_fixture.sqlite3 < fixture.sql
-- Mirrors the real KOReader schema (user_version 20240905) with representative rows:
--   beheld    normal row, highlight NULL (word == surface), has context
--   gouge(1)  highlight != word AND "(N)" suffix (surface = "gouging")
--   jilt      Dictionary-lookup row: empty context, highlight NULL
--   nook      null title_id (no book), has context
PRAGMA user_version = 20240905;
CREATE TABLE "vocabulary" (
    "word" TEXT NOT NULL UNIQUE, "title_id" INTEGER, "create_time" INTEGER NOT NULL,
    "review_time" INTEGER, "due_time" INTEGER NOT NULL, "review_count" INTEGER NOT NULL DEFAULT 0,
    "prev_context" TEXT, "next_context" TEXT, "streak_count" INTEGER NOT NULL DEFAULT 0,
    "highlight" TEXT, PRIMARY KEY("word"));
CREATE TABLE "title" (
    "id" INTEGER NOT NULL UNIQUE, "name" TEXT UNIQUE, "filter" INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY("id"));
INSERT INTO title (id, name) VALUES (1, 'A Court of Wings and Ruin'), (2, 'Dictionary lookup');
INSERT INTO vocabulary (word, title_id, create_time, due_time, prev_context, next_context, highlight)
  VALUES ('beheld', 1, 1700000001, 1700000001, 'She', 'the ruin before her.', NULL);
INSERT INTO vocabulary (word, title_id, create_time, due_time, prev_context, next_context, highlight)
  VALUES ('gouge(1)', 1, 1700000002, 1700000002, 'a wall of white bone,', 'deep.', 'gouging');
INSERT INTO vocabulary (word, title_id, create_time, due_time, prev_context, next_context, highlight)
  VALUES ('jilt', 2, 1700000003, 1700000003, '', '', NULL);
INSERT INTO vocabulary (word, title_id, create_time, due_time, prev_context, next_context, highlight)
  VALUES ('nook', NULL, 1700000004, 1700000004, 'a cozy', 'by the fire.', NULL);
