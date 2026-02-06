-- Migration: Add activity column to threads table
-- Version: 012
-- Date: 2026-02-06
--
-- Stores a short LLM-generated summary of the latest activity in each thread.
-- Updated after each chat round using a small model (e.g. claude-haiku-4).

ALTER TABLE threads ADD COLUMN activity TEXT;
