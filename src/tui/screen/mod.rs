//! The review screen's state: what is selected, how far the detail pane is
//! scrolled, what a first key press armed, and what the last frame showed.
//!
//! Keys act on the last frame, not on a fresh look at the engine: what a
//! person pressed a key at is what they saw. The frame is at most a tenth of
//! a second old, and the engine checks again before it acts.

use super::actions;
use super::detail::{self, Context};
use super::input::{Edit, Input};
use super::keys::{self, Action, Armed, Intent, Pending, Press};
use super::layout;
use super::list;
use super::help;
mod mine;
use mine::{TabState, TaskPick};
use super::mine_view::{self, Tab};
use super::model::{self, Archived, Row, Section, Sources, Wait};
use super::terminal::{self, Term};
use super::text::expand_tabs;
use crate::job::{Job, JobState};
use crate::prlist::PrInfo;
use crate::report::{sanitize_block, sanitize_for_display};
use crate::rundir;
use crate::ui::{SPINNER_FRAMES, count, fmt_dur};
use crossterm::event::{self, Event, MouseButton, MouseEvent, MouseEventKind};
use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Style, Stylize};
use ratatui::text::Line;
use ratatui::widgets::{Block, Borders, Padding, Paragraph, Wrap};
use std::collections::HashMap;
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::sync::mpsc::{Receiver, Sender, channel};
use std::time::{Duration, Instant};

/// How long a message stays in the footer.
const MESSAGE_FOR: Duration = Duration::from_secs(5);
/// The most of the run log the log view reads.
const LOG_TAIL_BYTES: u64 = 64 * 1024;

pub struct Header {
    /// owner/name, for the header and for `gh --repo`.
    pub repo: String,
    /// What the run does, in a few words: one pass, babysitting, watching.
    pub mode: String,
    pub repo_root: PathBuf,
    pub log: PathBuf,
    /// Whether the run looks for work again after a pass.
    pub looping: bool,
    /// The tasks this run will not start, and why: a skill that is not
    /// installed, a run under --no-post. Said at the key, before a pass is
    /// spent finding out.
    pub task_refusals: Vec<(crate::task::Task, String)>,
}

/// What the right pane shows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Side {
    /// The selected PR.
    Detail,
    /// The end of the run log: `l`.
    Log,
    /// Every key: `?`.
    Keys,
}

impl Side {
    /// The pane a key asks for: the one named, or back to the PR when it is
    /// already showing.
    fn toggle(self, to: Side) -> Side {
        if self == to { Side::Detail } else { to }
    }
}

/// The selected row as the last frame drew it, and what each key would do.
#[derive(Clone)]
struct Picked {
    pr: u64,
    resume: Result<String, String>,
    stop: Result<(), String>,
    request: Result<(), String>,
}

pub struct Screen {
    term: Option<Term>,
    header: Header,
    selected: Option<u64>,
    /// The PR the detail pane was last drawn for, and its section. A new
    /// one starts at the top, or at the end for a running review; so does a
    /// review that has just finished, whose pane now reads from the top.
    drawn_for: Option<(u64, Section)>,
    list_offset: usize,
    scroll: usize,
    /// Keep the detail pane at its end as new activity arrives.
    follow: bool,
    /// How far the detail pane could scroll, and half its height, as of
    /// the last frame.
    scroll_max: usize,
    page: usize,
    side: Side,
    armed: Option<Armed>,
    message: Option<(String, Instant)>,
    frame: usize,
    waiting: Vec<(u64, Wait)>,
    info: HashMap<u64, PrInfo>,
    next_check: Option<i64>,
    busy: Option<String>,
    ended: Option<String>,
    /// The selected review's text, by the path it was read from. None
    /// inside when the file was not there to read.
    review: Option<(PathBuf, Option<Vec<String>>)>,
    /// The tail of the run log, and the log's length when it was read.
    log: (u64, Vec<String>),
    said: (Sender<String>, Receiver<String>),
    order: Vec<u64>,
    picked: Option<Picked>,
    running: usize,
    /// Where the two panes were drawn, for working out what the pointer is
    /// over. The detail pane's is the area inside its border, which is what
    /// its scrolling is measured in.
    list_area: Rect,
    detail_area: Rect,
    /// Whether the screen is taking the mouse. Off hands drags back to the
    /// terminal, which is how text is selected and copied.
    mouse: bool,
    /// What the reviewers are told to look at, as the engine last said.
    focus: Option<String>,
    /// The focus being typed. While this is open every key belongs to it.
    editing: Option<Input>,
    /// Which list is shown, and what the other one remembers.
    tab: Tab,
    stash: TabState,
    /// Your own open PRs, as the latest look found them.
    mine: Vec<crate::mine::MyPr>,
    task_pick: Option<TaskPick>,
    /// Tasks making their worktree, as the pool last said. They count as
    /// running for the quit.
    preparing: usize,
    /// Your babysat PRs, with the fixes each has had, as the loop said.
    babysat: Vec<(u64, u32)>,
    /// Whether every PR of yours is babysat, including ones opened later.
    babysat_all: bool,
}

/// Whether a point is inside an area. The pointer arrives in screen
/// coordinates, the same ones the areas were drawn in.
fn inside(area: Rect, (column, row): (u16, u16)) -> bool {
    area.width > 0
        && area.height > 0
        && column >= area.x
        && column < area.x + area.width
        && row >= area.y
        && row < area.y + area.height
}

impl Screen {
    /// Take the terminal. See `terminal` for what that does to fds 1 and 2.
    pub fn open(header: Header) -> std::io::Result<Screen> {
        let term = Term::open(&header.log)?;
        Ok(Screen::new(Some(term), header))
    }

    fn new(term: Option<Term>, header: Header) -> Screen {
        Screen {
            term,
            header,
            selected: None,
            drawn_for: None,
            list_offset: 0,
            scroll: 0,
            follow: false,
            scroll_max: 0,
            page: 1,
            side: Side::Detail,
            armed: None,
            message: None,
            frame: 0,
            waiting: Vec::new(),
            info: HashMap::new(),
            next_check: None,
            busy: None,
            ended: None,
            review: None,
            log: (0, Vec::new()),
            said: channel(),
            order: Vec::new(),
            picked: None,
            running: 0,
            list_area: Rect::ZERO,
            detail_area: Rect::ZERO,
            mouse: true,
            focus: None,
            editing: None,
            tab: Tab::Review,
            stash: TabState::default(),
            mine: Vec::new(),
            task_pick: None,
            preparing: 0,
            babysat: Vec::new(),
            babysat_all: false,
        }
    }

    pub fn set_preparing(&mut self, n: usize) {
        self.preparing = n;
    }

    /// What `x` stops on the tab shown: a task on My PRs, a review on Review.
    fn job_word(&self) -> &'static str {
        match self.tab {
            Tab::Mine => "task",
            Tab::Review => "review",
        }
    }

    pub fn set_waiting(&mut self, waiting: Vec<(u64, Wait)>) {
        self.waiting = waiting;
    }

    pub fn know(&mut self, info: &HashMap<u64, PrInfo>) {
        self.info.extend(info.iter().map(|(pr, i)| (*pr, i.clone())));
    }

    pub fn set_next_check(&mut self, at: Option<i64>) {
        self.next_check = at;
    }

    pub fn set_busy(&mut self, what: Option<&str>) {
        self.busy = what.map(String::from);
    }

    /// What the reviewers are being told to look at, as the engine has it.
    /// The screen shows it and opens its editor on it; the engine decides
    /// what it actually is.
    pub fn set_focus(&mut self, focus: Option<&str>) {
        self.focus = focus.map(String::from);
    }

    /// What the run does now, for the header, and whether it looks for work
    /// again after a pass. Both change when `w` is pressed.
    pub fn set_mode(&mut self, mode: &str, looping: bool) {
        self.header.mode = mode.to_string();
        self.header.looping = looping;
    }

    /// The run has nothing left to do; the screen stays, saying so, until a
    /// key ends it or asks for a review. None again once one does.
    pub fn set_ended(&mut self, why: Option<&str>) {
        self.ended = why.map(String::from);
    }

    /// A line for the footer, until the next one or a few seconds pass.
    pub fn flash(&mut self, msg: impl Into<String>) {
        self.message = Some((sanitize_for_display(&msg.into()), Instant::now()));
    }

    /// Draw a frame. Nothing once the terminal has been given back, which a
    /// panic on another thread may have done.
    pub fn draw(&mut self, jobs: &[Job], pass_dir: &Path, archive: &[Archived]) {
        if !terminal::is_open() {
            return;
        }
        let Some(mut term) = self.term.take() else { return };
        let _ = term.terminal.draw(|f| self.render(f, jobs, pass_dir, archive));
        self.term = Some(term);
    }

    /// Every key since the last call, turned into what the engine should do.
    /// Keys that only change the view are handled here. So is what the
    /// background actions said since the last call.
    pub fn events(&mut self) -> Vec<Action> {
        while let Ok(said) = self.said.1.try_recv() {
            self.flash(said);
        }
        let mut out = Vec::new();
        while event::poll(Duration::ZERO).unwrap_or(false) {
            let Ok(ev) = event::read() else { break };
            match ev {
                // While the focus is being typed, every key is the
                // editor's: j and k are letters, not moves.
                Event::Key(key) if self.editing.is_some() => out.extend(self.edit(key)),
                Event::Key(key) => {
                    if let Some(intent) = keys::intent(key, self.tab == Tab::Mine) {
                        out.extend(self.press(intent, Instant::now()));
                    }
                }
                Event::Mouse(mouse) => self.point(mouse),
                _ => {}
            }
        }
        out
    }

    fn render(&mut self, f: &mut Frame, jobs: &[Job], pass_dir: &Path, archive: &[Archived]) {
        // Moved out for the frame, so the rows can borrow them while the
        // rest of the screen's state changes.
        let waiting = std::mem::take(&mut self.waiting);
        let info = std::mem::take(&mut self.info);
        let mine = std::mem::take(&mut self.mine);
        {
            let src = Sources { jobs, pass_dir, archive, waiting: &waiting, info: &info };
            let rows = model::rows(&src);
            // Counted from every review and every task, whichever tab is
            // shown: q on My PRs must not quit past a running review.
            self.running = rows.iter().filter(|r| r.running()).count() + mine_view::running(jobs) + self.preparing;
            let counts = (rows.len(), mine.len());
            match self.tab {
                Tab::Review => self.render_rows(f, &rows, counts),
                Tab::Mine => {
                    let mine_rows = mine_view::rows(&mine, jobs, pass_dir, archive, &self.babysat);
                    self.render_mine(f, &rows, &mine_rows, counts);
                }
            }
        }
        self.waiting = waiting;
        self.info = info;
        self.mine = mine;
    }

    /// The header and the tab bar, the same on both tabs.
    fn draw_top(&self, f: &mut Frame, areas: &layout::Areas, counts: (usize, usize)) {
        let log = self.header.log.display().to_string();
        let header = layout::header(&self.header.repo, &self.header.mode, self.focus.as_deref(), &log, areas.header.width as usize);
        f.render_widget(header, areas.header);
        let bar = mine_view::tab_bar(self.tab, counts.0, counts.1, self.babysat.len(), areas.tabs.width as usize);
        f.render_widget(bar, areas.tabs);
    }

    /// The right pane, scrolled as the keys left it.
    fn draw_detail(&mut self, f: &mut Frame, areas: &layout::Areas, body: Vec<Line<'static>>) {
        let borders = if areas.detail.x > areas.list.x { Borders::LEFT } else { Borders::TOP };
        let block = Block::new()
            .borders(borders)
            .border_style(Style::new().dark_gray())
            .padding(Padding::left(1));
        let inner = block.inner(areas.detail);
        self.detail_area = inner;
        let paragraph = Paragraph::new(body).wrap(Wrap { trim: false });
        let total = paragraph.line_count(inner.width);
        let height = inner.height as usize;
        self.page = (height / 2).max(1);
        self.scroll_max = total.saturating_sub(height);
        self.scroll = if self.follow { self.scroll_max } else { self.scroll.min(self.scroll_max) };
        let scroll = u16::try_from(self.scroll).unwrap_or(u16::MAX);
        f.render_widget(paragraph.scroll((scroll, 0)).block(block), areas.detail);
    }

    /// The footer: the status, or the latest message, and the keys.
    fn draw_footer(&mut self, f: &mut Frame, areas: &layout::Areas, rows: &[Row], now: i64) {
        if self.message.as_ref().is_some_and(|(_, at)| at.elapsed() > MESSAGE_FOR) {
            self.message = None;
        }
        // While a focus is being typed the footer is the line it is typed
        // on: the keys it lists are letters until enter or esc.
        let width = areas.footer.width as usize;
        let footer = match &self.editing {
            Some(input) => layout::prompt(&input.text(), input.cursor(), width),
            None => {
                let status = self.status(rows, now);
                let hints = if self.tab == Tab::Mine { layout::MINE_HINTS } else { layout::HINTS };
                layout::footer(&status, self.message.as_ref().map(|(m, _)| m.as_str()), width, hints)
            }
        };
        f.render_widget(footer, areas.footer);
    }

    fn render_rows(&mut self, f: &mut Frame, rows: &[Row], counts: (usize, usize)) {
        let now = crate::clock::epoch_secs();
        self.frame += 1;
        let spinner = SPINNER_FRAMES[self.frame % SPINNER_FRAMES.len()];
        let at = model::position(rows, self.selected);
        let row = at.map(|i| &rows[i]);
        self.selected = row.map(|r| r.pr);
        self.order = rows.iter().map(|r| r.pr).collect();
        self.task_pick = None;
        let shown = row.map(|r| (r.pr, r.section));
        if self.drawn_for != shown {
            self.drawn_for = shown;
            self.scroll = 0;
            self.follow = row.is_some_and(|r| r.section == Section::Running);
        }
        self.picked = row.map(|r| self.pick(r));
        let review_path = row
            .and_then(|r| r.last)
            .filter(|l| l.job.state == JobState::Done)
            .map(|l| rundir::review_file(l.pass_dir, l.job.pr));
        self.load_review(review_path);

        let areas = layout::areas(f.area());
        self.draw_top(f, &areas, counts);

        let (lines, selected_line) = list::lines(rows, at, areas.list.width as usize, spinner, now);
        let height = areas.list.height as usize;
        self.list_offset = list::offset(selected_line, height, self.list_offset, lines.len());
        let visible: Vec<Line> = lines.into_iter().skip(self.list_offset).take(height).collect();
        self.list_area = areas.list;
        f.render_widget(Paragraph::new(visible), areas.list);

        let body = self.detail_lines(row, now);
        self.draw_detail(f, &areas, body);
        self.draw_footer(f, &areas, rows, now);
    }

    fn pick(&self, row: &Row) -> Picked {
        Picked {
            pr: row.pr,
            resume: row.resumable().and_then(|r| actions::resume_line(r.job, &self.header.repo_root)),
            stop: row.stoppable(),
            request: row.requestable(self.header.looping),
        }
    }

    fn detail_lines(&mut self, row: Option<&Row>, now: i64) -> Vec<Line<'static>> {
        match self.side {
            Side::Log => return self.log_lines(),
            Side::Keys => return help::lines(),
            Side::Detail => {}
        }
        let Some(row) = row else {
            return vec![Line::from("no PRs to show yet").dark_gray()];
        };
        let ctx = Context {
            now,
            repo_root: &self.header.repo_root,
            review: self.review.as_ref().and_then(|(_, text)| text.as_deref()),
            next_check: self.next_check,
        };
        detail::lines(row, &ctx)
    }

    fn load_review(&mut self, path: Option<PathBuf>) {
        let Some(path) = path else {
            self.review = None;
            return;
        };
        // Read once per selection, and again only while it is not there: a
        // review's text is written once, before its row says it is done.
        let cached = self.review.as_ref().is_some_and(|(p, text)| *p == path && text.is_some());
        if cached {
            return;
        }
        let text = std::fs::read_to_string(&path)
            .ok()
            .map(|raw| sanitize_block(&raw).lines().map(expand_tabs).collect());
        self.review = Some((path, text));
    }

    fn log_lines(&mut self) -> Vec<Line<'static>> {
        let len = std::fs::metadata(&self.header.log).map_or(0, |m| m.len());
        if len != self.log.0 {
            self.log = (len, read_tail(&self.header.log, len));
        }
        let mut out = vec![Line::from(format!("run log · {}", self.header.log.display())).bold().dark_gray()];
        out.extend(self.log.1.iter().map(|l| Line::from(l.clone())));
        out
    }

    fn status(&self, rows: &[Row], now: i64) -> String {
        if let Some(ended) = &self.ended {
            return format!("{ended} · q quits");
        }
        let [running, queued, waiting, finished] = model::counts(rows);
        let mut parts: Vec<String> = [(running, "running"), (queued, "queued"), (waiting, "waiting"), (finished, "finished")]
            .iter()
            .filter(|(n, _)| *n > 0)
            .map(|(n, word)| format!("{n} {word}"))
            .collect();
        if parts.is_empty() {
            parts.push("nothing to review yet".into());
        }
        match (&self.busy, self.next_check) {
            (Some(busy), _) => parts.push(format!("{busy}…")),
            (None, Some(at)) => parts.push(format!("next check in {}", fmt_dur(at.saturating_sub(now).max(0) as u64))),
            _ => {}
        }
        parts.join(" · ")
    }

    /// One key of a focus being typed. Enter hands it to the engine, esc
    /// leaves the focus as it was, and a sentence the flag would refuse is
    /// refused here too rather than reaching a prompt.
    fn edit(&mut self, key: crossterm::event::KeyEvent) -> Vec<Action> {
        let Some(input) = &mut self.editing else { return Vec::new() };
        match input.key(key) {
            Edit::Typing => Vec::new(),
            Edit::Cancelled => {
                self.editing = None;
                self.flash("the focus is unchanged");
                Vec::new()
            }
            Edit::Done(text) if text.trim().is_empty() => {
                self.editing = None;
                self.focus = None;
                self.flash("the reviewers are told nothing in particular now");
                vec![Action::Focus(None)]
            }
            Edit::Done(text) => match crate::cli::parse_focus(&text) {
                Ok(focus) => {
                    self.editing = None;
                    // Shown at once. The engine says the same thing back
                    // when it takes it, which is what keeps them in step.
                    self.focus = Some(focus.clone());
                    self.flash(format!("the reviewers are told: {focus}"));
                    vec![Action::Focus(Some(focus))]
                }
                Err(why) => {
                    self.flash(why);
                    Vec::new()
                }
            },
        }
    }

    /// One mouse event. Nothing it does needs the engine: it selects a row
    /// or scrolls a pane, both of which are this screen's own business.
    fn point(&mut self, mouse: MouseEvent) {
        let at = (mouse.column, mouse.row);
        match mouse.kind {
            // The wheel scrolls whatever it is pointing at. Three lines a
            // notch is what the rest of the terminal does.
            MouseEventKind::ScrollDown if inside(self.detail_area, at) => self.scroll_by(3),
            MouseEventKind::ScrollUp if inside(self.detail_area, at) => self.scroll_by(-3),
            MouseEventKind::ScrollDown if inside(self.list_area, at) => self.move_by(1),
            MouseEventKind::ScrollUp if inside(self.list_area, at) => self.move_by(-1),
            MouseEventKind::Down(MouseButton::Left) if inside(self.list_area, at) => {
                let row =
                    list::row_at(mouse.row, self.list_area.y, self.list_offset, self.order.len());
                if let Some(pr) = row.and_then(|at| self.order.get(at)) {
                    self.selected = Some(*pr);
                }
            }
            _ => {}
        }
    }

    /// Scroll the detail pane by `lines`, and follow its end again once the
    /// scrolling reaches it -- a running review keeps arriving at the
    /// bottom, and a person who scrolled there is asking to stay there.
    fn scroll_by(&mut self, lines: isize) {
        self.scroll = self.scroll.saturating_add_signed(lines).min(self.scroll_max);
        self.follow = self.scroll >= self.scroll_max;
    }

    /// Put `side` in the right pane, from its top. The log follows its end
    /// as lines arrive; coming back to the PR draws it afresh.
    fn show(&mut self, side: Side) {
        self.side = side;
        self.scroll = 0;
        self.follow = side == Side::Log;
        if side == Side::Detail {
            self.drawn_for = None;
        }
    }

    fn move_by(&mut self, delta: isize) {
        if let Some(pr) = model::step(&self.order, self.selected, delta) {
            self.selected = Some(pr);
        }
    }

    /// One key. What it means for the engine is returned; the rest happens
    /// here.
    fn press(&mut self, intent: Intent, now: Instant) -> Vec<Action> {
        if keys::disarm(intent) {
            self.armed = None;
        }
        let picked = self.picked.clone();
        match intent {
            Intent::Up => self.move_by(-1),
            Intent::Down => self.move_by(1),
            Intent::Top => self.selected = self.order.first().copied(),
            Intent::Bottom => self.selected = self.order.last().copied(),
            Intent::PageUp => {
                self.follow = false;
                self.scroll = self.scroll.saturating_sub(self.page);
            }
            Intent::PageDown => {
                self.scroll = (self.scroll + self.page).min(self.scroll_max);
                self.follow = self.scroll >= self.scroll_max;
            }
            Intent::Log => {
                self.show(self.side.toggle(Side::Log));
            }
            Intent::Keys => {
                self.show(self.side.toggle(Side::Keys));
            }
            Intent::SwitchTab => self.switch_tab(),
            Intent::Fix => return self.ask_task(crate::task::Task::Fix),
            Intent::Comments => return self.ask_task(crate::task::Task::Comments),
            Intent::Conflicts => return self.ask_task(crate::task::Task::Conflicts),
            Intent::Babysit => return self.toggle_babysit(),
            Intent::BabysitAll => return self.toggle_babysit_all(),
            Intent::Back => {
                if self.side != Side::Detail {
                    self.side = Side::Detail;
                    self.drawn_for = None;
                }
            }
            Intent::Resume => match picked.map(|p| (p.pr, p.resume)) {
                Some((pr, Ok(line))) => {
                    actions::resume_in_tab(pr, line, self.said.0.clone());
                    self.flash(format!("opening PR #{pr}'s review in a new tab"));
                }
                Some((_, Err(why))) => self.flash(why),
                None => {}
            },
            Intent::Open => {
                if let Some(p) = picked {
                    actions::open_in_browser(p.pr, self.header.repo.clone(), self.said.0.clone());
                    self.flash(format!("opening PR #{} in the browser", p.pr));
                }
            }
            Intent::Stop => match picked.map(|p| (p.pr, p.stop)) {
                Some((pr, Ok(()))) => match keys::confirm(self.armed, Pending::Stop(pr), now) {
                    Press::Arm(armed) => {
                        self.armed = Some(armed);
                        self.flash(format!("press x again to stop PR #{pr}'s {}", self.job_word()));
                    }
                    Press::Fire => {
                        self.armed = None;
                        self.flash(format!("stopping PR #{pr}'s {}", self.job_word()));
                        return vec![Action::StopJob(pr)];
                    }
                },
                Some((_, Err(why))) => self.flash(why),
                None => {}
            },
            // The run's own looking for work, turned on or off. What it is
            // now comes from the engine, which says so through set_mode.
            Intent::Watch => {
                let on = !self.header.looping;
                self.flash(if on {
                    "watching: the run will look for work until you press w again"
                } else {
                    "no longer looking for work; the reviews running will finish"
                });
                return vec![Action::Watch(on)];
            }
            Intent::ReviewNow => match picked.map(|p| (p.pr, p.request)) {
                Some((pr, Ok(()))) => {
                    self.flash(format!("PR #{pr} is reviewed next"));
                    return vec![Action::ReviewNow(pr)];
                }
                Some((_, Err(why))) => self.flash(why),
                None => {}
            },
            Intent::Quit => {
                if self.ended.is_some() || self.running == 0 {
                    return vec![Action::Stop];
                }
                match keys::confirm(self.armed, Pending::Quit, now) {
                    Press::Arm(armed) => {
                        self.armed = Some(armed);
                        self.flash(format!("press q again to stop {} and quit", count(self.running, "running job")));
                    }
                    Press::Fire => return vec![Action::Stop],
                }
            }
            // The terminal cannot select text while the screen is taking
            // the mouse, so the key that hands it back is how a person
            // copies a session id or a resume command out of the pane.
            Intent::Mouse => {
                self.mouse = !self.mouse;
                if let Some(term) = &mut self.term {
                    let _ = term.set_mouse(self.mouse);
                }
                self.flash(if self.mouse {
                    "the screen has the mouse again"
                } else {
                    "the mouse is the terminal's: drag to select text, m to take it back"
                });
            }
            // What the reviewers are told to look at, typed in place. The
            // engine takes it from the action and says what it became.
            Intent::Focus => {
                self.editing = Some(Input::at_end(self.focus.as_deref().unwrap_or("")));
            }
            Intent::Interrupt => return vec![Action::Stop],
        }
        Vec::new()
    }
}

/// The last lines of a file, cleaned for the screen. A read that starts
/// mid-file drops its first, partial line.
fn read_tail(path: &Path, len: u64) -> Vec<String> {
    let Ok(mut file) = std::fs::File::open(path) else { return Vec::new() };
    let from = len.saturating_sub(LOG_TAIL_BYTES);
    if file.seek(SeekFrom::Start(from)).is_err() {
        return Vec::new();
    }
    let mut bytes = Vec::new();
    if file.read_to_end(&mut bytes).is_err() {
        return Vec::new();
    }
    let text = String::from_utf8_lossy(&bytes);
    let skip = usize::from(from > 0);
    text.lines().skip(skip).map(|l| sanitize_for_display(&expand_tabs(l))).collect()
}

#[cfg(test)]
mod tests;
