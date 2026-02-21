# User Story: Generic Branch Detection

**ID:** 003  
**Status:** Completed  
**Priority:** Medium  
**Epic:** Branch Type Detection System

## Story

As a developer without git-flow-next installed,  
I want git-hop to detect common branch prefixes,  
So that I can still benefit from branch type awareness.

## Acceptance Criteria

- [x] Generic detector recognizes: feature/, release/, hotfix/, support/, bugfix/
- [x] Works when git-flow-next is not installed
- [x] Lower priority than git-flow-next detector
- [x] Provides branch type info without running git-flow commands

## Technical Implementation

- Created `internal/detector/generic.go` with `GenericDetector`
- Default config includes common branch types
- No-op OnAdd/OnRemove (doesn't run any commands)
- Priority 100 (runs after git-flow-next at priority 10)

## Default Configuration

| Type    | Prefix    | Parent   | Start Point |
|---------|-----------|----------|-------------|
| feature | feature/  | develop  | develop     |
| release | release/  | main     | develop     |
| hotfix  | hotfix/   | main     | main        |
| support | support/  | main     | main        |
| bugfix  | bugfix/   | develop  | develop     |

## Tests

- Unit: `internal/detector/detector_test.go` - `TestGenericDetector_*`, `TestDefaultGenericConfig`
