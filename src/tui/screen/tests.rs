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
        task_refusals: Vec::new(),
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
    // Two lines a row, most pressing first, and no headings.
    assert!(out.contains("#9 · reviewing 0s"), "{out}");
    assert!(out.contains("@alice Change 9"), "{out}");
    assert!(out.contains("#8 · queued"), "{out}");
    assert!(out.contains("#7 · approved"), "{out}");
    assert!(!out.contains("RUNNING") && !out.contains("FINISHED"), "{out}");
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
    assert!(out.contains("#7 · approved") && out.contains("LAST REVIEW"), "{out}");
}

#[test]
fn stopping_a_review_takes_two_presses_on_the_same_pr() {
    let mut screen = Screen::new(None, header(true));
    let jobs = vec![job(9, JobState::Running)];
    frame(&mut screen, &jobs, &[], 120);
    let t0 = Instant::now();
    assert!(screen.press(Intent::Stop, t0).is_empty());
    assert!(screen.message.as_ref().unwrap().0.contains("press x again"));
    assert_eq!(screen.press(Intent::Stop, t0), vec![Action::StopJob(9)]);
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
    assert!(screen.message.as_ref().unwrap().0.contains("stop 1 running job and quit"));
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
fn w_turns_the_looking_for_work_on_and_off() {
    let mut screen = Screen::new(None, header(false));
    frame(&mut screen, &[], &[], 120);
    assert_eq!(screen.press(Intent::Watch, Instant::now()), vec![Action::Watch(true)]);
    assert!(screen.message.as_ref().unwrap().0.contains("look for work"));
    // The engine says what it did; the header follows it, not the key.
    screen.set_mode("watching every 2m", true);
    let out = frame(&mut screen, &[], &[], 120);
    assert!(out.contains("watching every 2m"), "{out}");
    assert_eq!(screen.press(Intent::Watch, Instant::now()), vec![Action::Watch(false)]);
    assert!(screen.message.as_ref().unwrap().0.contains("no longer looking"));
}

fn wheel(kind: MouseEventKind, column: u16, row: u16) -> MouseEvent {
    MouseEvent { kind, column, row, modifiers: crossterm::event::KeyModifiers::NONE }
}

#[test]
fn the_wheel_scrolls_whichever_pane_it_points_at() {
    let mut screen = Screen::new(None, header(true));
    let mut long = done(7);
    long.job.title = "Change 7".into();
    frame(&mut screen, &[], &[long], 120);
    // The detail pane is taller than its content here, so there is
    // nothing to scroll and the wheel leaves it at the end.
    screen.scroll_max = 40;
    screen.follow = false;
    let over_detail = (screen.detail_area.x + 2, screen.detail_area.y + 1);
    screen.point(wheel(MouseEventKind::ScrollDown, over_detail.0, over_detail.1));
    assert_eq!(screen.scroll, 3, "three lines a notch");
    screen.point(wheel(MouseEventKind::ScrollUp, over_detail.0, over_detail.1));
    assert_eq!(screen.scroll, 0);
    screen.point(wheel(MouseEventKind::ScrollUp, over_detail.0, over_detail.1));
    assert_eq!(screen.scroll, 0, "and never past the top");
}

#[test]
fn the_wheel_over_the_list_moves_the_selection() {
    let mut screen = Screen::new(None, header(true));
    let jobs = vec![job(9, JobState::Running), job(8, JobState::Queued)];
    frame(&mut screen, &jobs, &[], 120);
    let over_list = (screen.list_area.x + 1, screen.list_area.y + 1);
    screen.point(wheel(MouseEventKind::ScrollDown, over_list.0, over_list.1));
    assert_eq!(screen.selected, Some(8));
    screen.point(wheel(MouseEventKind::ScrollUp, over_list.0, over_list.1));
    assert_eq!(screen.selected, Some(9));
}

#[test]
fn a_click_selects_the_row_it_lands_on() {
    let mut screen = Screen::new(None, header(true));
    let jobs = vec![job(9, JobState::Running), job(8, JobState::Queued)];
    frame(&mut screen, &jobs, &[], 120);
    let x = screen.list_area.x + 1;
    // Each row is two lines: the second row starts two lines down.
    screen.point(wheel(MouseEventKind::Down(MouseButton::Left), x, screen.list_area.y + 2));
    assert_eq!(screen.selected, Some(8));
    screen.point(wheel(MouseEventKind::Down(MouseButton::Left), x, screen.list_area.y));
    assert_eq!(screen.selected, Some(9));
    // Past the last row, and outside the pane: nothing moves.
    screen.point(wheel(MouseEventKind::Down(MouseButton::Left), x, screen.list_area.y + 9));
    screen.point(wheel(MouseEventKind::Down(MouseButton::Left), screen.detail_area.x + 2, screen.list_area.y + 2));
    assert_eq!(screen.selected, Some(9));
}

#[test]
fn m_hands_the_mouse_back_to_the_terminal() {
    let mut screen = Screen::new(None, header(true));
    frame(&mut screen, &[], &[done(7)], 120);
    assert!(screen.mouse, "the screen takes it to begin with");
    assert!(screen.press(Intent::Mouse, Instant::now()).is_empty());
    assert!(!screen.mouse);
    assert!(screen.message.as_ref().unwrap().0.contains("drag to select text"));
    screen.press(Intent::Mouse, Instant::now());
    assert!(screen.mouse);
}

#[test]
fn a_focus_is_typed_in_place_and_handed_to_the_engine() {
    use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};
    let mut screen = Screen::new(None, header(true));
    screen.set_focus(Some("the ledger"));
    frame(&mut screen, &[], &[done(7)], 120);
    assert!(screen.press(Intent::Focus, Instant::now()).is_empty());
    // The editor opens on what the run is already being told.
    let out = frame(&mut screen, &[], &[done(7)], 120);
    assert!(out.contains("focus the ledger"), "{out}");
    assert!(out.contains("enter to apply"), "{out}");

    // Every key is the editor's now, j and k included.
    let key = |c: char| KeyEvent::new(KeyCode::Char(c), KeyModifiers::NONE);
    for c in " jk".chars() {
        assert!(screen.edit(key(c)).is_empty());
    }
    let done_key = KeyEvent::new(KeyCode::Enter, KeyModifiers::NONE);
    assert_eq!(
        screen.edit(done_key),
        vec![Action::Focus(Some("the ledger jk".into()))],
        "what was typed, as the flag would have taken it"
    );
    assert!(screen.editing.is_none(), "and the editor closes");

    // Esc leaves it alone; an empty line clears it.
    screen.press(Intent::Focus, Instant::now());
    screen.edit(key('x'));
    assert!(screen.edit(KeyEvent::new(KeyCode::Esc, KeyModifiers::NONE)).is_empty());
    screen.press(Intent::Focus, Instant::now());
    for _ in 0..40 {
        screen.edit(KeyEvent::new(KeyCode::Backspace, KeyModifiers::NONE));
    }
    assert_eq!(screen.edit(done_key), vec![Action::Focus(None)]);
}

#[test]
fn the_header_shows_what_the_reviewers_are_told() {
    let mut screen = Screen::new(None, header(true));
    let out = frame(&mut screen, &[], &[done(7)], 120);
    assert!(!out.contains("focus:"), "nothing to say yet: {out}");
    screen.set_focus(Some("be strict about the ledger"));
    let out = frame(&mut screen, &[], &[done(7)], 120);
    assert!(out.contains("focus: be strict about the ledger"), "{out}");
}

#[test]
fn what_review_now_takes() {
    // A looping run has dropped what it lists as finished.
    let mut screen = Screen::new(None, header(true));
    frame(&mut screen, &[], &[done(7)], 120);
    assert!(screen.press(Intent::ReviewNow, Instant::now()).is_empty());
    assert!(screen.message.as_ref().unwrap().0.contains("finished for this run"));

    let mut waiting = Screen::new(None, header(true));
    waiting.set_waiting(vec![(5, Wait::Quiet)]);
    frame(&mut waiting, &[], &[], 120);
    assert_eq!(waiting.press(Intent::ReviewNow, Instant::now()), vec![Action::ReviewNow(5)]);

    // A one-shot run reviews a PR it has already reviewed, on request.
    let mut once = Screen::new(None, header(false));
    frame(&mut once, &[], &[done(7)], 120);
    assert_eq!(once.press(Intent::ReviewNow, Instant::now()), vec![Action::ReviewNow(7)]);
}

#[test]
fn an_ended_run_says_so_and_quits_at_once() {
    let mut screen = Screen::new(None, header(true));
    screen.set_ended(Some("nothing left to babysit"));
    screen.set_waiting(vec![(5, Wait::Quiet)]);
    let out = frame(&mut screen, &[], &[done(7)], 120);
    assert!(out.contains("nothing left to babysit · q quits"), "{out}");
    assert_eq!(screen.press(Intent::Quit, Instant::now()), vec![Action::Stop]);
    // A run waiting for q still takes a request: it is what starts
    // another pass. #7 is reviewed and above #5, which is only quiet,
    // and a key acts on the frame the person saw -- so move, draw, ask.
    screen.press(Intent::Down, Instant::now());
    frame(&mut screen, &[], &[done(7)], 120);
    assert_eq!(screen.press(Intent::ReviewNow, Instant::now()), vec![Action::ReviewNow(5)]);
    screen.set_ended(None);
    let out = frame(&mut screen, &[], &[done(7)], 120);
    assert!(!out.contains("q quits"), "{out}");
}

#[test]
fn the_status_says_what_the_loop_is_doing() {
    let mut screen = Screen::new(None, header(true));
    let now = crate::clock::epoch_secs();
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

fn my_pr(n: u64) -> crate::mine::MyPr {
    crate::mine::MyPr {
        number: n,
        title: "My own work".into(),
        draft: false,
        branch: "me/my-own-work".into(),
        cross_repo: false,
        review: crate::mine::Review::Required,
        merge: crate::mine::Merge::Clean,
        ci: crate::ci::Ci::Passing,
        open_threads: 0,
        reviewers: Vec::new(),
    }
}

#[test]
fn tab_shows_your_prs_and_each_tab_keeps_its_selection() {
    let mut screen = Screen::new(None, header(true));
    screen.set_mine(vec![my_pr(4), my_pr(11)]);
    let archive = vec![done(7), done(6)];
    let out = frame(&mut screen, &[], &archive, 120);
    assert!(out.contains("Review 2") && out.contains("My PRs 2"), "{out}");
    screen.press(Intent::Down, Instant::now());
    frame(&mut screen, &[], &archive, 120);
    let review_pick = screen.selected;
    screen.press(Intent::SwitchTab, Instant::now());
    let out = frame(&mut screen, &[], &archive, 120);
    assert!(out.contains("#4 · awaiting review"), "{out}");
    assert!(out.contains("b babysits it"), "the detail says what b does: {out}");
    assert_eq!(screen.selected, Some(4), "the first row: the list comes ranked from the fetch");
    screen.press(Intent::Down, Instant::now());
    frame(&mut screen, &[], &archive, 120);
    let mine_pick = screen.selected;
    screen.press(Intent::SwitchTab, Instant::now());
    frame(&mut screen, &[], &archive, 120);
    assert_eq!(screen.selected, review_pick, "the review tab comes back where it was");
    screen.press(Intent::SwitchTab, Instant::now());
    frame(&mut screen, &[], &archive, 120);
    assert_eq!(screen.selected, mine_pick);
}

#[test]
fn b_and_c_ask_for_a_task_on_your_pr_and_only_there() {
    let mut screen = Screen::new(None, header(true));
    screen.set_mine(vec![my_pr(4)]);
    frame(&mut screen, &[], &[done(7)], 120);
    assert!(screen.press(Intent::Babysit, Instant::now()).is_empty(), "not on the review tab");
    screen.press(Intent::SwitchTab, Instant::now());
    frame(&mut screen, &[], &[], 120);
    let asked = screen.press(Intent::Babysit, Instant::now());
    let [Action::RunTask(request)] = asked.as_slice() else { panic!("{asked:?}") };
    assert_eq!((request.pr, request.task), (4, crate::task::Task::Fix));
    let asked = screen.press(Intent::Comments, Instant::now());
    assert!(matches!(asked.as_slice(), [Action::RunTask(r)] if r.task == crate::task::Task::Comments));
    // R does not review your own PR, and says what does.
    assert!(screen.press(Intent::ReviewNow, Instant::now()).is_empty());
    let out = frame(&mut screen, &[], &[], 120);
    assert!(out.contains("PR #4 is yours"), "{out}");
}

#[test]
fn a_task_the_run_refuses_is_refused_at_the_key() {
    let refusals = vec![(crate::task::Task::Fix, "babysit-pr is not installed where claude looks (~/.claude/skills)".to_string())];
    let mut screen = Screen::new(None, Header { task_refusals: refusals, ..header(true) });
    screen.set_mine(vec![my_pr(4)]);
    screen.press(Intent::SwitchTab, Instant::now());
    frame(&mut screen, &[], &[], 120);
    assert!(screen.press(Intent::Babysit, Instant::now()).is_empty());
    let out = frame(&mut screen, &[], &[], 120);
    assert!(out.contains("babysit-pr is not installed"), "{out}");
    assert_eq!(screen.press(Intent::Comments, Instant::now()).len(), 1, "the other task is not refused");
}

#[test]
fn quit_on_your_prs_still_asks_while_a_review_runs() {
    let mut screen = Screen::new(None, header(true));
    screen.set_mine(vec![my_pr(4)]);
    screen.press(Intent::SwitchTab, Instant::now());
    let jobs = vec![job(9, JobState::Running)];
    frame(&mut screen, &jobs, &[], 120);
    assert!(screen.press(Intent::Quit, Instant::now()).is_empty(), "the running review is not on this tab, and still counts");
    // A running task counts too.
    let mut task = job(4, JobState::Running);
    task.task = crate::task::Task::Fix;
    let mut screen = Screen::new(None, header(true));
    screen.set_mine(vec![my_pr(4)]);
    let out = frame(&mut screen, &[task.clone()], &[], 120);
    assert!(!out.contains("#4 · reviewing"), "a task is not a review row: {out}");
    assert!(screen.press(Intent::Quit, Instant::now()).is_empty());
}

#[test]
fn question_mark_lists_every_key_in_the_right_pane() {
    let mut screen = Screen::new(None, header(true));
    let archive = vec![done(7)];
    let out = frame(&mut screen, &[], &archive, 120);
    assert!(out.contains("? keys"), "the footer names it: {out}");
    screen.press(Intent::Keys, Instant::now());
    let out = frame(&mut screen, &[], &archive, 120);
    assert!(out.contains("KEYS"), "{out}");
    assert!(out.contains("review the selected PR now, even one that is capped or resting"), "{out}");
    assert!(out.contains("#7 · approved"), "the list stays: {out}");
    // ? again, or esc, puts it away.
    screen.press(Intent::Keys, Instant::now());
    assert!(!frame(&mut screen, &[], &archive, 120).contains("KEYS"));
    screen.press(Intent::Keys, Instant::now());
    screen.press(Intent::Back, Instant::now());
    assert!(!frame(&mut screen, &[], &archive, 120).contains("KEYS"));
    // l from the keys goes to the log, not back to the PR.
    screen.press(Intent::Keys, Instant::now());
    screen.press(Intent::Log, Instant::now());
    let out = frame(&mut screen, &[], &archive, 120);
    assert!(out.contains("run log") && !out.contains("KEYS"), "{out}");
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
