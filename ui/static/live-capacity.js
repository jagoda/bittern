// Bittern live capacity. One pure state machine decides how many automatic
// panes stay live (ADR 0037 section 4). The wall feeds it a view every two
// seconds and applies the one action it returns; nothing here touches the DOM,
// so the browser check can drive a synthetic frame-drop sequence through it.
//
// The numbers are the ADR's: a ten second window from six two second samples,
// hot above 2 percent in the current and the previous window, quiet below 0.5
// percent across sixty seconds, one demotion per twenty seconds, one promotion
// per sixty seconds and never sooner than three hundred seconds after the last
// demotion, priors of 4, 8 and 16 by viewport class, and a hard maximum of 16.
(function (global) {
  "use strict";

  var SAMPLE_MS = 2000;
  var WINDOW_MS = 10000;
  var QUIET_WINDOW_MS = 60000;
  var HISTORY_MS = 120000;
  var HOT_RATE = 0.02;
  var QUIET_RATE = 0.005;
  var DEMOTE_INTERVAL_MS = 20000;
  var PROMOTE_INTERVAL_MS = 60000;
  var QUIET_AFTER_DEMOTION_MS = 300000;

  var PRIOR_PHONE = 4;
  var PRIOR_TABLET = 8;
  var PRIOR_DESKTOP = 16;

  // The ADR 0037 device classes: below 44rem, 44rem to below 80rem, and 80rem
  // and above, in CSS pixels at the default root size.
  function priorForWidth(width) {
    if (width < 704) return PRIOR_PHONE;
    if (width < 1280) return PRIOR_TABLET;
    return PRIOR_DESKTOP;
  }

  function sampleAt(history, at) {
    var found = null;
    for (var index = 0; index < history.length; index += 1) {
      if (history[index].at <= at) found = history[index];
      else break;
    }
    return found;
  }

  // rateBetween returns dropped over total for the span, or null when the span
  // has no frames or no sample old enough to measure it.
  function rateBetween(history, from, to) {
    var start = sampleAt(history, from);
    var end = sampleAt(history, to);
    if (!start || !end || end === start) return null;
    var total = end.total - start.total;
    if (total <= 0) return null;
    var dropped = end.dropped - start.dropped;
    if (dropped < 0) return null;
    return dropped / total;
  }

  function create(options) {
    options = options || {};
    var limit = PRIOR_DESKTOP;
    var prior = options.prior || limit;
    var capacity = prior;
    var enabled = options.enabled !== false;
    var history = {};
    var demoted = {};
    var lastDemotionAt = -Infinity;
    var lastPromotionAt = -Infinity;
    var lastActionAt = -Infinity;

    function historyFor(id) {
      if (!history[id]) history[id] = [];
      return history[id];
    }

    function record(pane, at) {
      if (typeof pane.dropped !== "number" || typeof pane.total !== "number") {
        return;
      }
      var samples = historyFor(pane.id);
      var last = samples.length ? samples[samples.length - 1] : null;
      if (last && at <= last.at) return;
      samples.push({ at: at, dropped: pane.dropped, total: pane.total });
      while (samples.length > 1 && samples[0].at < at - HISTORY_MS) {
        samples.shift();
      }
    }

    function resetEvidence(paneId) {
      if (paneId && history[paneId]) history[paneId] = [];
      if (paneId) return;
      for (var id in history) {
        if (Object.prototype.hasOwnProperty.call(history, id)) history[id] = [];
      }
    }

    function automaticLivePanes(view) {
      return view.panes.filter(function (pane) {
        return !pane.manual && !demoted[pane.id];
      });
    }

    function isHot(id, at) {
      var samples = history[id] || [];
      var current = rateBetween(samples, at - WINDOW_MS, at);
      var previous = rateBetween(samples, at - 2 * WINDOW_MS, at - WINDOW_MS);
      return current !== null && previous !== null && current > HOT_RATE && previous > HOT_RATE;
    }

    function isQuiet(id, at) {
      var samples = history[id] || [];
      var rate = rateBetween(samples, at - QUIET_WINDOW_MS, at);
      return rate !== null && rate < QUIET_RATE;
    }

    // Areas within one percent are the same rendered size. A fractional pixel
    // in one column is not a different area, and reading order has to decide.
    function sameArea(first, second) {
      if (first === second) return true;
      var largest = Math.max(Math.abs(first), Math.abs(second), 1);
      return Math.abs(first - second) <= largest * 0.01;
    }

    function leastVisible(panes) {
      var best = null;
      panes.forEach(function (pane) {
        if (!best) {
          best = pane;
          return;
        }
        if (!sameArea(pane.area, best.area) && pane.area < best.area) {
          best = pane;
          return;
        }
        // Ties go to the later pane in reading order.
        if (sameArea(pane.area, best.area) && pane.order > best.order) best = pane;
      });
      return best;
    }

    function mostVisible(panes) {
      var best = null;
      panes.forEach(function (pane) {
        if (!best) {
          best = pane;
          return;
        }
        if (!sameArea(pane.area, best.area) && pane.area > best.area) {
          best = pane;
          return;
        }
        // Ties go to the earlier pane in reading order.
        if (sameArea(pane.area, best.area) && pane.order < best.order) best = pane;
      });
      return best;
    }

    var model = {
      // setPrior is the viewport class change. A smaller class is an immediate
      // clamp of the least visible automatic panes; a larger class only raises
      // the cap, so no pane starts until the promotion conditions are met.
      setPrior: function (nextPrior, at, view) {
        nextPrior = Math.max(1, Math.min(limit, nextPrior));
        if (nextPrior === prior) return 0;
        prior = nextPrior;
        resetEvidence();
        if (capacity > prior) capacity = prior;
        if (!view) return 0;
        return model.clamp(at, view);
      },

      // clamp demotes least visible automatic panes until the automatic live
      // count fits the capacity. It is the burst-free path: it never starts.
      clamp: function (at, view) {
        var demotions = 0;
        var automatic = automaticLivePanes(view);
        while (automatic.length > capacity) {
          var target = leastVisible(automatic);
          if (!target) break;
          demoted[target.id] = true;
          automatic = automaticLivePanes(view);
          demotions += 1;
        }
        if (demotions) {
          lastDemotionAt = at;
          resetEvidence();
        }
        return demotions;
      },

      evaluate: function (at, view) {
        if (!enabled) return null;
        view.panes.forEach(function (pane) {
          record(pane, at);
        });
        var live = view.panes.filter(function (pane) {
          return pane.live;
        });
        var hot = live.filter(function (pane) {
          return isHot(pane.id, at);
        });
        if (hot.length) {
          if (at - lastActionAt < DEMOTE_INTERVAL_MS) return null;
          // 1. Step a hot automatic main pane down to sub before any demotion.
          var main = hot.filter(function (pane) {
            return !pane.manual && pane.quality === "main" && !demoted[pane.id];
          });
          if (main.length) {
            var step = mostVisible(main);
            resetEvidence(step.id);
            lastActionAt = at;
            return { kind: "step-down", id: step.id };
          }
          // 2. Demote one automatic live pane, never a manual one.
          var automatic = automaticLivePanes(view).filter(function (pane) {
            return pane.live;
          });
          if (!automatic.length) return null;
          var target = leastVisible(automatic);
          if (!target) return null;
          demoted[target.id] = true;
          capacity = Math.max(0, capacity - 1);
          lastDemotionAt = at;
          lastActionAt = at;
          resetEvidence();
          return { kind: "demote", id: target.id, last: automatic.length === 1 };
        }
        // 3. Promote one capacity-demoted automatic pane, slowly.
        if (at - lastDemotionAt < QUIET_AFTER_DEMOTION_MS) return null;
        if (at - lastPromotionAt < PROMOTE_INTERVAL_MS) return null;
        if (capacity >= prior) return null;
        var candidates = view.panes.filter(function (pane) {
          return !pane.manual && demoted[pane.id];
        });
        if (!candidates.length) return null;
        if (!live.length) return null;
        for (var index = 0; index < live.length; index += 1) {
          if (!isQuiet(live[index].id, at)) return null;
        }
        var promote = mostVisible(candidates);
        delete demoted[promote.id];
        capacity = Math.min(prior, capacity + 1);
        lastPromotionAt = at;
        resetEvidence();
        return { kind: "promote", id: promote.id };
      },

      // recordManual is the wall telling the model the owner chose a quality.
      // A manual live pane is never a demotion or promotion target.
      setManual: function (id, manual) {
        if (manual) {
          delete demoted[id];
        }
      },

      isDemoted: function (id) {
        return demoted[id] === true;
      },

      prior: function () {
        return prior;
      },

      capacity: function () {
        return capacity;
      },

      setEnabled: function (value) {
        enabled = value !== false;
      },

      isEnabled: function () {
        return enabled;
      },

      rates: function (id, at) {
        var samples = history[id] || [];
        return {
          current: rateBetween(samples, at - WINDOW_MS, at),
          previous: rateBetween(samples, at - 2 * WINDOW_MS, at - WINDOW_MS),
          quiet: rateBetween(samples, at - QUIET_WINDOW_MS, at),
          hot: isHot(id, at),
        };
      },

      reset: function () {
        resetEvidence();
      },

      constants: {
        sampleMs: SAMPLE_MS,
        windowMs: WINDOW_MS,
        quietWindowMs: QUIET_WINDOW_MS,
        hotRate: HOT_RATE,
        quietRate: QUIET_RATE,
        demoteIntervalMs: DEMOTE_INTERVAL_MS,
        promoteIntervalMs: PROMOTE_INTERVAL_MS,
        quietAfterDemotionMs: QUIET_AFTER_DEMOTION_MS,
      },
    };
    return model;
  }

  global.BitternCapacity = {
    create: create,
    priorForWidth: priorForWidth,
    PRIOR_PHONE: PRIOR_PHONE,
    PRIOR_TABLET: PRIOR_TABLET,
    PRIOR_DESKTOP: PRIOR_DESKTOP,
  };
})(typeof window === "undefined" ? globalThis : window);
