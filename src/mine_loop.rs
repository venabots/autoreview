//! The run loop's side of the My PRs tab: reading your PRs at each look,
//! and asking for a fix on each babysat PR that is due one.
//!
//! The loop owns this, not the view: babysitting outlives a key press, and
//! only the loop knows when a look happened and when a fix ended.

use crate::babysit::{Babysat, Change};
use crate::job::Job;
use crate::mine::{self, MyPr};
use crate::pool;
use crate::repo::RepoContext;
use crate::task::{Request, Task};
use crate::ui::Ui;
use std::sync::mpsc::Receiver;

#[derive(Default)]
pub struct MineLoop {
    /// Whether the last look at your PRs failed, so a failure is said once.
    failing: bool,
    /// Your PRs as the last look that worked found them.
    list: Vec<MyPr>,
    babysat: Babysat,
}

impl MineLoop {
    /// Read your PRs, for the My PRs tab. Only with the view up: nothing
    /// else shows them, and a run in a pipe must not spend a call on them.
    /// A failed look keeps the last list and says so once, until a look
    /// works again.
    pub fn refresh(&mut self, ctx: &RepoContext, ui: &mut Ui, rx: &Receiver<pool::Event>) {
        if !ui.ticking() {
            return;
        }
        match ui.while_busy("reading your PRs", rx, || mine::fetch(ctx)) {
            Ok(list) => {
                self.failing = false;
                self.list = list;
                ui.mine(self.list.clone());
                self.show(ui);
            }
            Err(e) => {
                if !self.failing {
                    ui.note(format!("note: could not read your PRs ({e:#}); the My PRs tab keeps its last list"));
                }
                self.failing = true;
            }
        }
    }

    /// Apply what `b` and `B` asked for. True when something is babysat:
    /// the run must then keep looking for work, or no fix would ever come.
    pub fn apply(&mut self, ui: &mut Ui) -> bool {
        let changes = ui.take_babysit_changes();
        if changes.is_empty() {
            return self.babysat.any();
        }
        for change in changes {
            match change {
                Change::One(pr, on) => self.babysat.set(pr, on, &self.list),
                Change::All(on) => self.babysat.set_all(on),
            }
        }
        self.show(ui);
        self.babysat.any()
    }

    /// Ask for a fix on each babysat PR that is due one. A PR with a task
    /// already waiting is busy, and waits for the next look.
    ///
    /// Not after a failed look: the list is the last good one, and a fix
    /// that just ended would take its state from before the fix.
    /// Nor while `w` waits to turn the looking off: the run is stopping.
    pub fn ask_due(&mut self, ui: &mut Ui) {
        if self.failing || ui.watch_toggle_peek() == Some(false) {
            return;
        }
        // A stop pressed while the look ran is applied first, so the PR it
        // names is not fixed on its way out.
        self.apply(ui);
        let due = self.babysat.due(&self.list, |pr| ui.task_waiting(pr));
        for pr in due {
            let Some(found) = self.list.iter().find(|p| p.number == pr) else { continue };
            println!("babysitting: fixing PR #{pr}, which changed and needs work");
            ui.run_task(Request {
                pr,
                task: Task::Fix,
                title: found.title.clone(),
                branch: found.branch.clone(),
                cross_repo: found.cross_repo,
            });
        }
        self.show(ui);
    }

    /// A pass ended. A fix on a babysat PR is over: its own push must not
    /// read as a change at the next look.
    pub fn ended(&mut self, jobs: &[Job]) {
        for job in jobs.iter().filter(|j| j.task == Task::Fix) {
            self.babysat.fixed(job.pr);
        }
    }

    /// Whether anything is babysat: the run keeps looking while it is.
    pub fn any(&self) -> bool {
        self.babysat.any()
    }

    /// Stop babysitting everything. For `w` turning the looking off:
    /// babysitting with no looks would say it babysits and never fix.
    ///
    /// A `b` pressed in the same pass is dropped too: applying it after this
    /// would turn the looking back on.
    pub fn stop(&mut self, ui: &mut Ui) {
        let pending = !ui.take_babysit_changes().is_empty();
        if self.babysat.any() || pending {
            self.babysat.set_all(false);
            println!("no longer babysitting your PRs: the run stopped looking for work");
        }
        self.show(ui);
    }

    fn show(&self, ui: &mut Ui) {
        ui.babysat(self.babysat.listed(&self.list), self.babysat.all());
    }
}
