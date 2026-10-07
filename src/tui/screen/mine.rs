//! The My PRs half of the screen: the list of your own PRs, what each tab
//! remembers while the other one is shown, and the keys that start a task.
//!
//! A child of `screen`, so it reaches the screen's own state: the two tabs
//! share the panes, the footer and the selection, and only what fills them
//! differs.

use super::{Picked, Screen, Side};
use crate::job::JobState;
use crate::tui::Action;
use crate::tui::actions;
use crate::tui::help;
use crate::tui::layout;
use crate::tui::list;
use crate::tui::mine_view::{self, MineRow, Tab};
use crate::tui::model::{Row, Section};
use crate::ui::SPINNER_FRAMES;
use ratatui::Frame;
use ratatui::style::Stylize;
use ratatui::text::Line;
use ratatui::widgets::Paragraph;

/// What a tab remembers while the other one is shown: which row was
/// selected and how far its panes were scrolled.
#[derive(Debug, Clone, Default)]
pub(super) struct TabState {
    pub(super) selected: Option<u64>,
    pub(super) drawn_for: Option<(u64, Section)>,
    pub(super) list_offset: usize,
    pub(super) scroll: usize,
    pub(super) follow: bool,
}

/// The selected one of your PRs, as `b` and `c` would ask for it.
#[derive(Clone)]
pub(super) struct TaskPick {
    pub(super) fix: crate::task::Request,
    pub(super) comments: crate::task::Request,
    pub(super) busy: bool,
}

impl Screen {
    /// Your open PRs, replacing the last list: one merged or closed since
    /// must leave the tab.
    pub fn set_mine(&mut self, mine: Vec<crate::mine::MyPr>) {
        self.mine = mine;
    }

    /// Which of your PRs are babysat, as the loop decided.
    pub fn set_babysat(&mut self, babysat: Vec<(u64, u32)>, all: bool) {
        self.babysat = babysat;
        self.babysat_all = all;
    }

    /// Show the other tab, and pick up where it was left.
    pub(super) fn switch_tab(&mut self) {
        let now = TabState {
            selected: self.selected,
            drawn_for: self.drawn_for,
            list_offset: self.list_offset,
            scroll: self.scroll,
            follow: self.follow,
        };
        let back = std::mem::replace(&mut self.stash, now);
        self.selected = back.selected;
        self.drawn_for = back.drawn_for;
        self.list_offset = back.list_offset;
        self.scroll = back.scroll;
        self.follow = back.follow;
        self.tab = self.tab.other();
    }

    /// The My PRs tab. The footer still describes the review run: it is
    /// what is running, whichever list is shown.
    pub(super) fn render_mine(&mut self, f: &mut Frame, rows: &[Row], mine: &[MineRow], counts: (usize, usize)) {
        let now = crate::clock::epoch_secs();
        self.frame += 1;
        let spinner = SPINNER_FRAMES[self.frame % SPINNER_FRAMES.len()];
        let at = self
            .selected
            .and_then(|pr| mine.iter().position(|r| r.pr.number == pr))
            .or(if mine.is_empty() { None } else { Some(0) });
        let row = at.map(|i| &mine[i]);
        self.selected = row.map(|r| r.pr.number);
        self.order = mine.iter().map(|r| r.pr.number).collect();
        let shown = row.map(|r| (r.pr.number, Section::Finished));
        if self.drawn_for != shown {
            self.drawn_for = shown;
            self.scroll = 0;
            self.follow = false;
        }
        self.picked = row.map(|r| self.pick_mine(r));
        self.task_pick = row.map(|r| TaskPick {
            fix: r.request(crate::task::Task::Fix),
            comments: r.request(crate::task::Task::Comments),
            busy: r.busy(),
        });

        let areas = layout::areas(f.area());
        self.draw_top(f, &areas, counts);
        let (lines, selected_line) = mine_view::lines(mine, at, areas.list.width as usize, spinner);
        let height = areas.list.height as usize;
        self.list_offset = list::offset(selected_line, height, self.list_offset, lines.len());
        let visible: Vec<Line> = lines.into_iter().skip(self.list_offset).take(height).collect();
        self.list_area = areas.list;
        f.render_widget(Paragraph::new(visible), areas.list);
        let body = match (self.side, row) {
            (Side::Log, _) => self.log_lines(),
            (Side::Keys, _) => help::lines(),
            (Side::Detail, Some(row)) => mine_view::detail(row),
            (Side::Detail, None) => vec![Line::from("you have no open PRs in this repo").dark_gray()],
        };
        self.draw_detail(f, &areas, body);
        self.draw_footer(f, &areas, rows, now);
    }

    /// What `r`, `x` and `R` do on one of your PRs. `r` reopens the last
    /// task's session in its worktree, while the worktree is there: a
    /// session belongs to the directory it ran in.
    fn pick_mine(&self, row: &MineRow) -> Picked {
        let pr = row.pr.number;
        let resume = match (row.live, row.last.as_ref()) {
            (Some(_), _) => Err(format!("PR #{pr} has a task running; resume it when it ends")),
            (None, Some((job, _))) => match job.cwd.as_deref().filter(|d| d.is_dir()) {
                Some(dir) => actions::resume_line(job, dir),
                None => Err(match &job.sid {
                    Some(sid) => format!("PR #{pr}'s task worktree is gone; its session was {sid}"),
                    None => format!("PR #{pr}'s task has no session to resume"),
                }),
            },
            (None, None) => Err(format!("PR #{pr} has had no task in this run")),
        };
        let stop = if row.live.is_some_and(|j| j.state == JobState::Running && !j.reaped) {
            Ok(())
        } else {
            Err(format!("PR #{pr} has no task running"))
        };
        Picked { pr, resume, stop, request: Err(mine_view::not_a_review(pr)) }
    }

    /// `b` or `c`: a task on the selected one of your PRs, or why not.
    pub(super) fn ask_task(&mut self, task: crate::task::Task) -> Vec<Action> {
        if self.tab != Tab::Mine {
            self.flash("b and c work on your own PRs: tab shows them");
            return Vec::new();
        }
        let Some(pick) = self.task_pick.clone() else {
            self.flash("you have no open PRs in this repo");
            return Vec::new();
        };
        let request = match task {
            crate::task::Task::Comments => pick.comments,
            _ => pick.fix,
        };
        let pr = request.pr;
        if pick.busy {
            self.flash(format!("PR #{pr} has a task running"));
            return Vec::new();
        }
        if let Some((_, why)) = self.header.task_refusals.iter().find(|(t, _)| *t == task) {
            self.flash(why.clone());
            return Vec::new();
        }
        self.flash(format!("{} PR #{pr} next", task.doing()));
        vec![Action::RunTask(request)]
    }
}
