//! The review screen's state: what is selected, how far the detail pane is
//! scrolled, what a first key press armed, and what the last frame showed.
//!
//! Keys act on the last frame, not on a fresh look at the engine: what a
//! person pressed a key at is what they saw. The frame is at most a tenth of
//! a second old, and the engine checks again before it acts.

use super::actions;
use super::detail::{self, Context};
use super::keys::{self, Armed, Intent, Pending, Press};
use super::layout;
use super::list;
use super::model::{self, Archived, Row, Section, Sources, Wait};
use super::terminal::{self, Term};
use super::text::expand_tabs;
use crate::board::Action;
use crate::job::{Job, JobState};
use crate::prlist::PrInfo;
use crate::report::{sanitize_block, sanitize_for_display};
use crate::rundir;
use crate::ui::{SPINNER_FRAMES, count, fmt_dur};
use crossterm::event::{self, Event};
use ratatui::Frame;
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
    show_log: bool,
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
}

fn epoch_now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
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
            show_log: false,
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

    /// The run has nothing left to do; the screen stays until `q`.
    pub fn set_ended(&mut self, why: &str) {
        self.ended = Some(why.to_string());
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
            if let Event::Key(key) = ev
                && let Some(intent) = keys::intent(key)
            {
                out.extend(self.press(intent, Instant::now()));
            }
        }
        out
    }

    fn render(&mut self, f: &mut Frame, jobs: &[Job], pass_dir: &Path, archive: &[Archived]) {
        // Moved out for the frame, so the rows can borrow them while the
        // rest of the screen's state changes.
        let waiting = std::mem::take(&mut self.waiting);
        let info = std::mem::take(&mut self.info);
        {
            let src = Sources { jobs, pass_dir, archive, waiting: &waiting, info: &info };
            let rows = model::rows(&src);
            self.render_rows(f, &rows);
        }
        self.waiting = waiting;
        self.info = info;
    }

    fn render_rows(&mut self, f: &mut Frame, rows: &[Row]) {
        let now = epoch_now();
        self.frame += 1;
        let spinner = SPINNER_FRAMES[self.frame % SPINNER_FRAMES.len()];
        let at = model::position(rows, self.selected);
        let row = at.map(|i| &rows[i]);
        self.selected = row.map(|r| r.pr);
        self.order = rows.iter().map(|r| r.pr).collect();
        self.running = rows.iter().filter(|r| r.running()).count();
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
        let log = self.header.log.display().to_string();
        f.render_widget(layout::header(&self.header.repo, &self.header.mode, &log, areas.header.width as usize), areas.header);

        let (lines, selected_line) = list::lines(rows, at, areas.list.width as usize, spinner, now);
        let height = areas.list.height as usize;
        self.list_offset = list::offset(selected_line, height, self.list_offset, lines.len());
        let visible: Vec<Line> = lines.into_iter().skip(self.list_offset).take(height).collect();
        f.render_widget(Paragraph::new(visible), areas.list);

        let borders = if areas.detail.x > areas.list.x { Borders::LEFT } else { Borders::TOP };
        let block = Block::new()
            .borders(borders)
            .border_style(Style::new().dark_gray())
            .padding(Padding::left(1));
        let inner = block.inner(areas.detail);
        let body = self.detail_lines(row, now);
        let paragraph = Paragraph::new(body).wrap(Wrap { trim: false });
        let total = paragraph.line_count(inner.width);
        let height = inner.height as usize;
        self.page = (height / 2).max(1);
        self.scroll_max = total.saturating_sub(height);
        self.scroll = if self.follow { self.scroll_max } else { self.scroll.min(self.scroll_max) };
        let scroll = u16::try_from(self.scroll).unwrap_or(u16::MAX);
        f.render_widget(paragraph.scroll((scroll, 0)).block(block), areas.detail);

        if self.message.as_ref().is_some_and(|(_, at)| at.elapsed() > MESSAGE_FOR) {
            self.message = None;
        }
        let status = self.status(rows, now);
        let message = self.message.as_ref().map(|(m, _)| m.as_str());
        f.render_widget(layout::footer(&status, message, areas.footer.width as usize), areas.footer);
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
        if self.show_log {
            return self.log_lines();
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

    fn move_by(&mut self, delta: isize) {
        let at = self.selected.and_then(|pr| self.order.iter().position(|p| *p == pr)).unwrap_or(0);
        let to = at.saturating_add_signed(delta).min(self.order.len().saturating_sub(1));
        if let Some(pr) = self.order.get(to) {
            self.selected = Some(*pr);
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
                self.show_log = !self.show_log;
                self.scroll = 0;
                self.follow = self.show_log;
            }
            Intent::Back => {
                if self.show_log {
                    self.show_log = false;
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
                        self.flash(format!("press x again to stop PR #{pr}'s review"));
                    }
                    Press::Fire => {
                        self.armed = None;
                        self.flash(format!("stopping PR #{pr}'s review"));
                        return vec![Action::StopReview(pr)];
                    }
                },
                Some((_, Err(why))) => self.flash(why),
                None => {}
            },
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
                        self.flash(format!("press q again to stop {} and quit", count(self.running, "running review")));
                    }
                    Press::Fire => return vec![Action::Stop],
                }
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
mod tests {
    use super::*;
    use ratatui::Terminal;
    use ratatui::backend::TestBackend;

    fn header(looping: bool) -> Header {
        Header {
            repo: "acme/app".into(),
            mode: "watching every 2m".into(),
            repo_root: PathBuf::from("/src/app"),
            log: PathBuf::from("/nonexistent/autoreview.log"),
            looping,
        }
    }

    fn job(pr: u64, state: JobState) -> Job {
        let mut job = Job::new(pr);
        job.state = state;
        job.title = format!("Change {pr}");
        job.author = "alice".into();
        if state == JobState::Running {
            job.pgid = Some(1);
        }
        job
    }

    fn done(pr: u64) -> Archived {
        let mut job = job(pr, JobState::Done);
        job.verdict = Some("approved".into());
        job.sid = Some("7442b624-5cba-5d44-ae67-9c390cfe70a1".into());
        Archived { job, pass_dir: PathBuf::from("/nonexistent/pass-1") }
    }

    fn screen_text(t: &Terminal<TestBackend>) -> String {
        let buf = t.backend().buffer();
        (0..buf.area.height)
            .map(|y| (0..buf.area.width).map(|x| buf[(x, y)].symbol().to_string()).collect::<String>())
            .collect::<Vec<_>>()
            .join("\n")
    }

    fn frame(screen: &mut Screen, jobs: &[Job], archive: &[Archived], width: u16) -> String {
        let mut t = Terminal::new(TestBackend::new(width, 24)).unwrap();
        t.draw(|f| screen.render(f, jobs, Path::new("/nonexistent/pass-2"), archive)).unwrap();
        screen_text(&t)
    }

    #[test]
    fn a_frame_has_the_list_the_detail_and_the_keys() {
        let mut screen = Screen::new(None, header(true));
        let jobs = vec![job(9, JobState::Running), job(8, JobState::Queued)];
        let archive = vec![done(7)];
        let out = frame(&mut screen, &jobs, &archive, 120);
        assert!(out.starts_with("autoreview · acme/app · watching every 2m"), "{out}");
        assert!(out.contains("RUNNING 1"), "{out}");
        assert!(out.contains("#9"), "{out}");
        assert!(out.contains("QUEUED 1"), "{out}");
        assert!(out.contains("FINISHED 1"), "{out}");
        // The first row is selected, and the detail pane is about it.
        assert!(out.contains("#9 Change 9"), "{out}");
        assert!(out.contains("reviewing 0s · claude"), "{out}");
        assert!(out.contains("1 running · 1 queued · 1 finished"), "{out}");
        assert!(out.contains("q quit"), "{out}");
    }

    #[test]
    fn the_keys_move_the_selection_and_the_detail_follows() {
        let mut screen = Screen::new(None, header(true));
        let jobs = vec![job(9, JobState::Running)];
        let archive = vec![done(7)];
        frame(&mut screen, &jobs, &archive, 120);
        assert!(screen.press(Intent::Down, Instant::now()).is_empty());
        let out = frame(&mut screen, &jobs, &archive, 120);
        assert!(out.contains("#7 Change 7"), "{out}");
        assert!(out.contains("claude --resume 7442b624"), "{out}");
        assert!(out.contains("no review text at /nonexistent/pass-1/pr-7.review.md"), "{out}");
        // Held at the end.
        screen.press(Intent::Down, Instant::now());
        assert_eq!(screen.selected, Some(7));
        screen.press(Intent::Top, Instant::now());
        assert_eq!(screen.selected, Some(9));
    }

    #[test]
    fn a_review_that_finishes_is_read_from_the_top() {
        let mut screen = Screen::new(None, header(true));
        let running = vec![job(9, JobState::Running)];
        frame(&mut screen, &running, &[], 120);
        assert!(screen.follow, "a running review follows its activity");
        let out = frame(&mut screen, &[], &[done(9)], 120);
        assert!(!screen.follow);
        assert_eq!(screen.scroll, 0);
        assert!(out.contains("#9 Change 9"), "{out}");
    }

    #[test]
    fn a_narrow_terminal_still_draws_both_panes() {
        let mut screen = Screen::new(None, header(true));
        let archive = vec![done(7)];
        let out = frame(&mut screen, &[], &archive, 60);
        assert!(out.contains("FINISHED 1") && out.contains("LAST REVIEW"), "{out}");
    }

    #[test]
    fn stopping_a_review_takes_two_presses_on_the_same_pr() {
        let mut screen = Screen::new(None, header(true));
        let jobs = vec![job(9, JobState::Running)];
        frame(&mut screen, &jobs, &[], 120);
        let t0 = Instant::now();
        assert!(screen.press(Intent::Stop, t0).is_empty());
        assert!(screen.message.as_ref().unwrap().0.contains("press x again"));
        assert_eq!(screen.press(Intent::Stop, t0), vec![Action::StopReview(9)]);
        // Any other key in between disarms.
        screen.press(Intent::Stop, t0);
        screen.press(Intent::Down, t0);
        assert!(screen.press(Intent::Stop, t0).is_empty());
    }

    #[test]
    fn a_finished_review_cannot_be_stopped() {
        let mut screen = Screen::new(None, header(true));
        frame(&mut screen, &[], &[done(7)], 120);
        assert!(screen.press(Intent::Stop, Instant::now()).is_empty());
        assert!(screen.message.as_ref().unwrap().0.contains("has no review running"));
    }

    #[test]
    fn quitting_asks_again_only_while_reviews_run() {
        let mut screen = Screen::new(None, header(true));
        frame(&mut screen, &[job(9, JobState::Running)], &[], 120);
        let t0 = Instant::now();
        assert!(screen.press(Intent::Quit, t0).is_empty());
        assert!(screen.message.as_ref().unwrap().0.contains("stop 1 running review and quit"));
        assert_eq!(screen.press(Intent::Quit, t0), vec![Action::Stop]);

        let mut idle = Screen::new(None, header(true));
        frame(&mut idle, &[], &[done(7)], 120);
        assert_eq!(idle.press(Intent::Quit, t0), vec![Action::Stop]);
        // ctrl-C never asks.
        let mut busy = Screen::new(None, header(true));
        frame(&mut busy, &[job(9, JobState::Running)], &[], 120);
        assert_eq!(busy.press(Intent::Interrupt, t0), vec![Action::Stop]);
    }

    #[test]
    fn review_now_needs_a_run_that_loops() {
        let mut screen = Screen::new(None, header(true));
        frame(&mut screen, &[], &[done(7)], 120);
        // Approved: the run is finished with it.
        assert!(screen.press(Intent::ReviewNow, Instant::now()).is_empty());
        let mut waiting = Screen::new(None, header(true));
        waiting.set_waiting(vec![(5, Wait::Quiet)]);
        frame(&mut waiting, &[], &[], 120);
        assert_eq!(waiting.press(Intent::ReviewNow, Instant::now()), vec![Action::ReviewNow(5)]);
        let mut once = Screen::new(None, header(false));
        once.set_waiting(vec![(5, Wait::Quiet)]);
        frame(&mut once, &[], &[], 120);
        assert!(once.press(Intent::ReviewNow, Instant::now()).is_empty());
        assert!(once.message.as_ref().unwrap().0.contains("--watch or --babysit"));
    }

    #[test]
    fn an_ended_run_says_so_and_quits_at_once() {
        let mut screen = Screen::new(None, header(true));
        screen.set_ended("nothing left to babysit");
        let out = frame(&mut screen, &[], &[done(7)], 120);
        assert!(out.contains("nothing left to babysit · q quits"), "{out}");
        assert_eq!(screen.press(Intent::Quit, Instant::now()), vec![Action::Stop]);
    }

    #[test]
    fn the_status_says_what_the_loop_is_doing() {
        let mut screen = Screen::new(None, header(true));
        let now = epoch_now();
        screen.set_next_check(Some(now + 100));
        let out = frame(&mut screen, &[], &[], 120);
        assert!(out.contains("nothing to review yet · next check in 1m"), "{out}");
        screen.set_busy(Some("checking the PR list"));
        let out = frame(&mut screen, &[], &[], 120);
        assert!(out.contains("checking the PR list…"), "{out}");
    }

    #[test]
    fn the_log_view_shows_the_end_of_the_run_log() {
        let dir = std::env::temp_dir().join(format!("ar-screen-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let log = dir.join("autoreview.log");
        std::fs::write(&log, "start   #9 (reviewing)\n\u{1b}[31mdone    #9 (3s)\n").unwrap();
        let mut screen = Screen::new(None, Header { log: log.clone(), ..header(true) });
        screen.press(Intent::Log, Instant::now());
        let out = frame(&mut screen, &[], &[], 120);
        assert!(out.contains("start   #9 (reviewing)"), "{out}");
        assert!(out.contains("[31mdone    #9 (3s)"), "the escape byte is gone: {out}");
        screen.press(Intent::Back, Instant::now());
        let out = frame(&mut screen, &[], &[], 120);
        assert!(!out.contains("start   #9"), "{out}");
    }

    #[test]
    fn a_long_tail_drops_its_partial_first_line() {
        let dir = std::env::temp_dir().join(format!("ar-tail-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let log = dir.join("big.log");
        let body = format!("{}\nlast line\n", "x".repeat(LOG_TAIL_BYTES as usize + 10));
        std::fs::write(&log, &body).unwrap();
        let lines = read_tail(&log, body.len() as u64);
        assert_eq!(lines, vec!["last line"]);
    }
}
