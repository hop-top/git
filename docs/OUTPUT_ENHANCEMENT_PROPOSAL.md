# Output Enhancement Proposal

Comprehensive plan for polishing git-hop CLI output using Bubbles, Lipgloss, and go-pretty.

## Visual Enhancement Strategy

| Command | Current Output | Proposed Enhancement | Components | Priority |
|---------|---------------|---------------------|------------|----------|
| **`git hop <uri>`** | Plain text messages | Multi-step progress + styled success | Spinner (clone), Progress (checkout), Multi-step (init), Success banner | 🔥 High |
| **`git hop <branch>`** | Text only | Multi-step with validation feedback | Multi-step (validate, create, setup), Status badges | 🔥 High |
| **`git hop list`** | Basic table | Rich table with status indicators | Styled table, Color-coded status, Icons (✓/✗/○) | 🔥 High |
| **`git hop status`** | Text sections | Card-based layout with sections | Styled panels, Status badges, Tree view | 🔥 High |
| **`git hop env start`** | Docker logs passthrough | Live service status | Spinner per service, Status table, Health indicators | 🟡 Medium |
| **`git hop env stop`** | Simple messages | Graceful shutdown progress | Multi-step progress, Service count | 🟡 Medium |
| **`git hop add`** | Plain creation flow | Validation + creation workflow | Multi-step (validate, create, link), Success card | 🔥 High |
| **`git hop remove`** | Confirmation + delete | Interactive confirmation + progress | Styled prompt, Deletion progress, Cleanup summary | 🟡 Medium |
| **`git hop prune`** | List of removed items | Scan + review + clean workflow | Progress bar (scan), Review table, Cleanup summary | 🟢 Low |
| **`git hop doctor`** | Check results list | Issue matrix with fix options | Status table, Fix buttons, Progress for fixes | 🟡 Medium |
| **`git hop migrate`** | Step messages | Migration wizard | Multi-step with backup, Progress per step, Rollback option | 🟢 Low |
| **`git hop init`** | Setup messages | Interactive setup wizard | Multi-step (validate, create, configure), Success banner | 🔥 High |
| **`git hop install-hooks`** | Success/fail message | Hook installation summary | Check marks per hook, Permission warnings | 🟢 Low |

---

## Detailed Enhancement Specifications

### 🔥 Priority 1: Core User Journey

#### 1. `git hop <uri>` - Repository Cloning

**Current:**
```
Cloning repository...
Created hopspace for 'main'
Worktree: ./main
```

**Proposed:**
```
┌─────────────────────────────────────────────────┐
│ Cloning github.com/org/repo                     │
└─────────────────────────────────────────────────┘

  ⠋ Cloning repository...
  ✓ Cloned repository (2.3s)

  ⠋ Checking out main branch...
  ✓ Checked out main (0.5s)

  ⠋ Setting up environment...
  ✓ Environment ready (1.2s)

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Repository Ready                              ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Hub Path    │ ~/code/org/repo                  ┃
┃  Worktree    │ ~/code/org/repo/main             ┃
┃  Branch      │ main                             ┃
┃  Services    │ api, db (ports: 11500-11502)     ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

Next: cd org/repo && git hop feature-branch
```

**Components:**
- Header box (lipgloss)
- Multi-step spinner progress
- Success card with styled table
- Next steps hint
- Timing information

---

#### 2. `git hop <branch>` - Worktree Creation

**Current:**
```
Creating worktree for 'feature-x'...
Environment generated
Worktree ready
```

**Proposed:**
```
Creating worktree: feature-x

  [1/4] ✓ Validating branch (0.1s)
  [2/4] ✓ Creating worktree (0.8s)
  [3/4] ✓ Allocating ports (0.2s)
  [4/4] ✓ Generating environment (0.3s)

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Worktree Ready: feature-x                     ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Path        │ ~/code/org/repo/feature-x        ┃
┃  Services    │ api (11500), db (11501)          ┃
┃  Volumes     │ 2 persistent volumes created     ┃
┃  Env File    │ .env generated                   ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

▶ cd feature-x && git hop env start
```

**Components:**
- Multi-step counter
- Success card
- Port allocation display
- Next action hint

---

#### 3. `git hop list` - Worktree Listing

**Current:**
```
main (active)
feature-x
bugfix-123
```

**Proposed:**
```
┏━━━━━━━━━━━━━┯━━━━━━━━━━┯━━━━━━━━━━━━━━┯━━━━━━━━━┯━━━━━━━━━━┓
┃ Branch      │ Status   │ Last Active   │ Env     │ Ports    ┃
┣━━━━━━━━━━━━━┿━━━━━━━━━━┿━━━━━━━━━━━━━━┿━━━━━━━━━┿━━━━━━━━━━┫
┃ main        │ ✓ Active │ 2m ago        │ ● Up    │ 11500-02 ┃
┃ feature-x   │ ○ Clean  │ 1h ago        │ ○ Down  │ 11503-05 ┃
┃ bugfix-123  │ ✗ Dirty  │ 3d ago        │ ○ Down  │ 11506-08 ┃
┃ hotfix-456  │ ○ Clean  │ 1w ago        │ ⚠ Error │ 11509-11 ┃
┗━━━━━━━━━━━━━┷━━━━━━━━━━┷━━━━━━━━━━━━━━┷━━━━━━━━━┷━━━━━━━━━━┛

Summary: 4 worktrees · 1 active · 2 environments running

Legend: ✓ Active  ○ Clean  ✗ Dirty  ● Running  ○ Stopped  ⚠ Error
```

**Components:**
- Rounded bordered table
- Status icons and colors
- Human-readable timestamps
- Summary footer
- Legend for symbols

**Colors:**
- Active: Green (✓)
- Clean: Blue (○)
- Dirty: Yellow (✗)
- Running: Green (●)
- Stopped: Gray (○)
- Error: Red (⚠)

---

#### 4. `git hop status` - Detailed Status

**Current:**
```
Branch: feature-x
Worktree: /path/to/worktree
Environment: running
Services: api, db
Ports: 11500-11502
```

**Proposed:**
```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ Worktree: feature-x                             ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

📁 Repository
  Branch         feature-x
  Remote         origin/feature-x (2 commits ahead)
  Status         ✗ Modified (3 files)
  Path           ~/code/org/repo/feature-x
  Last Active    5m ago

🐳 Environment
  Status         ● Running
  Started        2h ago
  Compose        docker-compose.yml

  Services:
  ├─ api        ● Running   11500   Health: ✓
  ├─ db         ● Running   11501   Health: ✓
  └─ cache      ○ Stopped   11502   Health: -

📦 Dependencies
  Status         ✓ Synced (node_modules.a3f8e2)
  Shared         Yes (with main, feature-y)
  Last Install   1d ago

💾 Volumes
  ├─ api_data      2.3 GB    ~/volumes/feature-x_api_data
  └─ db_data       512 MB    ~/volumes/feature-x_db_data

🔧 Configuration
  .env           ✓ Generated
  hop.json       ✓ Valid
```

**Components:**
- Titled sections with emoji headers
- Tree structure for hierarchical data
- Status indicators with colors
- File sizes and timestamps
- Health checks

---

### 🟡 Priority 2: Environment Management

#### 5. `git hop env start` - Starting Services

**Current:**
```
Starting services...
api started
db started
```

**Proposed:**
```
Starting environment for feature-x

  ⠋ Validating docker-compose.yml...
  ✓ Configuration valid (0.1s)

  Starting services:
  ├─ ⠋ api (pulling image...)
  ├─ ⠋ db (pulling image...)
  └─ ⠋ cache (pulling image...)

  ✓ api     started (11500)   Health: ✓
  ✓ db      started (11501)   Health: ✓
  ✓ cache   started (11502)   Health: ✓

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Environment Running                           ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Services    │ 3 running                        ┃
┃  Ports       │ 11500-11502                      ┃
┃  Logs        │ docker compose logs -f           ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

**Components:**
- Service tree with individual spinners
- Health check indicators
- Success card
- Helper commands

---

#### 6. `git hop env stop` - Stopping Services

**Current:**
```
Stopping services...
Stopped
```

**Proposed:**
```
Stopping environment for feature-x

  Gracefully stopping services:
  ├─ ✓ api     (stopped in 1.2s)
  ├─ ✓ db      (stopped in 0.8s)
  └─ ✓ cache   (stopped in 0.3s)

✓ Environment stopped (3 services)
```

**Components:**
- Tree structure
- Timing per service
- Summary count

---

### 🟢 Priority 3: Maintenance & Setup

#### 7. `git hop doctor` - Health Check

**Current:**
```
Checking git...
Checking docker...
Checking permissions...
```

**Proposed:**
```
Running diagnostics...

┏━━━━━━━━━━━━━━━━━━━━━┯━━━━━━━━━┯━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ Check               │ Status  │ Details                 ┃
┣━━━━━━━━━━━━━━━━━━━━━┿━━━━━━━━━┿━━━━━━━━━━━━━━━━━━━━━━━━┫
┃ Git Installation    │ ✓ Pass  │ v2.45.0                 ┃
┃ Git Version         │ ✓ Pass  │ >= 2.7 required         ┃
┃ Docker Available    │ ✓ Pass  │ 24.0.7                  ┃
┃ Worktree Integrity  │ ✓ Pass  │ 4/4 valid               ┃
┃ Config Files        │ ⚠ Warn  │ hop.json outdated       ┃
┃ Orphaned Symlinks   │ ✗ Fail  │ 2 broken links found    ┃
┃ Port Conflicts      │ ✓ Pass  │ No conflicts            ┃
┃ Volume Permissions  │ ✓ Pass  │ All writable            ┃
┗━━━━━━━━━━━━━━━━━━━━━┷━━━━━━━━━┷━━━━━━━━━━━━━━━━━━━━━━━━┛

Issues found: 1 error, 1 warning

⚠ Config Files: hop.json schema version is old
  → Run: git hop migrate

✗ Orphaned Symlinks: 2 broken symlinks detected
  → Run: git hop doctor --fix

Run with --fix to automatically repair issues.
```

**Components:**
- Check progress with spinner
- Results table with status colors
- Issue summary
- Actionable fix suggestions

---

#### 8. `git hop add` - Adding Worktree

**Current:**
```
Adding branch feature-x
Creating worktree...
Done
```

**Proposed:**
```
Adding worktree: feature-x

  [1/5] ✓ Validating branch name (0.1s)
  [2/5] ✓ Checking remote existence (0.3s)
  [3/5] ✓ Creating worktree (0.8s)
  [4/5] ✓ Allocating resources (0.4s)
  [5/5] ✓ Generating environment (0.2s)

┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ✓ Worktree Added: feature-x                     ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Path        │ ~/code/org/repo/feature-x        ┃
┃  Upstream    │ origin/feature-x (tracking)      ┃
┃  Services    │ api (11500), db (11501)          ┃
┃  Status      │ Ready for development            ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

**Components:**
- Validated multi-step
- Tracking information
- Resource summary

---

#### 9. `git hop remove` - Removing Worktree

**Current:**
```
Remove hub at /path/to/hub? (y/n)
Removing...
Done
```

**Proposed:**
```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃ ⚠ Confirm Removal                               ┃
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃  Target      │ feature-x worktree               ┃
┃  Path        │ ~/code/org/repo/feature-x        ┃
┃  Changes     │ ✗ 3 uncommitted files            ┃
┃  Services    │ ● 2 running (will stop)          ┃
┃  Volumes     │ 2.8 GB (will preserve)           ┃
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

⚠ Warning: You have uncommitted changes!

Continue? (y/n): y

Removing worktree...
  ├─ ✓ Stopping services (2.1s)
  ├─ ✓ Unlinking worktree (0.1s)
  ├─ ✓ Removing directory (0.3s)
  └─ ✓ Cleaning references (0.1s)

✓ Worktree removed: feature-x
```

**Components:**
- Warning card
- Impact summary
- Confirmation prompt
- Removal progress

---

#### 10. `git hop prune` - Cleanup

**Current:**
```
Scanning for orphaned worktrees...
Found 2 orphaned worktrees
Removing...
```

**Proposed:**
```
Scanning for orphaned resources...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 100%

Found orphaned resources:

┏━━━━━━━━━━━━━━━━━━━┯━━━━━━━━━┯━━━━━━━━━━━━━━━━━┓
┃ Resource          │ Type    │ Size            ┃
┣━━━━━━━━━━━━━━━━━━━┿━━━━━━━━━┿━━━━━━━━━━━━━━━━━┫
┃ old-feature       │ Symlink │ (broken)        ┃
┃ temp-worktree     │ Dir     │ 45 MB           ┃
┃ node_modules.abc  │ Deps    │ 823 MB          ┃
┗━━━━━━━━━━━━━━━━━━━┷━━━━━━━━━┷━━━━━━━━━━━━━━━━━┛

Total: 3 items (868 MB)

Remove orphaned resources? (y/n): y

Cleaning up...
  ├─ ✓ old-feature (0.1s)
  ├─ ✓ temp-worktree (0.4s)
  └─ ✓ node_modules.abc (1.2s)

✓ Cleanup complete: 3 items removed, 868 MB freed
```

**Components:**
- Scan progress bar
- Review table
- Size calculations
- Space savings summary

---

## Color Scheme

### Status Colors (Lipgloss)
- ✓ Success: `lipgloss.Color("2")` - Green
- ✗ Error: `lipgloss.Color("1")` - Red
- ⚠ Warning: `lipgloss.Color("3")` - Yellow
- ○ Neutral: `lipgloss.Color("8")` - Gray
- ● Active: `lipgloss.Color("2")` - Green
- 📦 Info: `lipgloss.Color("12")` - Blue
- 🔧 Config: `lipgloss.Color("5")` - Magenta

### Text Emphasis
- Header: Bold + `lipgloss.Color("212")` (Pink)
- Accent: `lipgloss.Color("205")` (Pink)
- Muted: `lipgloss.Color("240")` (Gray)
- Code/Paths: `lipgloss.Color("cyan")`

---

## Icons & Symbols

### Status Indicators
- `✓` - Success/Pass/Active
- `✗` - Error/Fail/Broken
- `⚠` - Warning/Attention needed
- `○` - Clean/Stopped/Neutral
- `●` - Running/Active
- `⠋` - Processing (spinner frames)

### Section Headers
- `📁` - Repository/Files
- `🐳` - Docker/Services
- `📦` - Dependencies/Packages
- `💾` - Storage/Volumes
- `🔧` - Configuration
- `🌐` - Network/Ports
- `⚡` - Performance/Speed

### Tree Elements
- `├─` - Branch item
- `└─` - Last item
- `│` - Vertical line
- `▶` - Action hint

---

## Implementation Priority

### Phase 1 (Week 1): Core Commands
1. ✅ Setup output infrastructure (DONE)
2. `git hop <uri>` - Clone with progress
3. `git hop <branch>` - Worktree creation
4. `git hop list` - Rich table

### Phase 2 (Week 2): Status & Environment
5. `git hop status` - Detailed view
6. `git hop env start` - Service progress
7. `git hop add` - Creation workflow

### Phase 3 (Week 3): Maintenance
8. `git hop doctor` - Health check table
9. `git hop remove` - Confirmation flow
10. `git hop prune` - Cleanup summary

---

## Code Structure

```
internal/output/
├── logger.go          ✅ Base logging (DONE)
├── spinner.go         ✅ Spinners (DONE)
├── progress.go        ✅ Progress bars (DONE)
├── tables.go          📝 Rich table helpers (NEW)
├── cards.go           📝 Info card layouts (NEW)
├── prompts.go         📝 Interactive confirmations (NEW)
└── icons.go           📝 Icon constants (NEW)
```

---

## Testing Strategy

1. **Visual Regression**: Screenshots of each output mode
2. **Mode Testing**: Verify human/JSON/porcelain/quiet modes
3. **TTY Detection**: Graceful degradation without TTY
4. **Width Handling**: Test narrow (80) and wide (120+) terminals
5. **Performance**: Ensure spinners don't block operations

---

## Documentation Needed

1. Style guide for contributors
2. Icon/emoji reference
3. Color palette reference
4. Output mode behavior guide
5. Accessibility considerations (color-blind safe)
