// Layout assertions for one loaded page. ADR 0025 makes page-level horizontal
// overflow a failure, a control outside the viewport a failure, and a control
// below its role's touch target a failure or a warning. A visible image is a
// replaced element, so it is measured separately: it must be decoded and its
// rendered box must fit in the viewport.
//
// The accepted touch-target rule is 44 by 44 CSS pixels for a control in a
// navigation, an action row, or a whole-row target, and 24 by 24 as the hard
// floor everywhere else, including links inside a sentence. Hit area counts and
// visual size does not, so a control inside its own wrapping label is measured
// by the label. TARGET_RULES is the one rule list: the first matching rule
// names the role and its target, and FLOOR is the fallback for anything left.
// A control that matches a 44 rule and the floor gets 44.
//
// A control below its target fails. A control in a floor role that reaches 24
// but not 44 warns, so an inline prose link stays visible as advisory.
//
// Controls are scrolled to the middle of the viewport before measuring, so a
// control that needs an ordinary vertical scroll is not treated as unreachable.
// A control that cannot be brought fully into view, or whose centre is covered
// by another element, is a failure.

const CONTROL_SELECTOR = [
  "a[href]",
  "button",
  "input:not([type='hidden'])",
  "select",
  "textarea",
  "[tabindex]:not([tabindex='-1'])",
  "[contenteditable='true']",
  "[contenteditable='']",
].join(", ");

// A replaced element is not a control, so images are measured separately. A
// visible image must be decoded and its rendered box must fit in the viewport.
const IMAGE_SELECTOR = "img";

// One readable role list. `containers` match the control or an ancestor with
// closest(); `controls` match the control itself. The order is the priority, so
// an element matching more than one role takes the first, and the floor is only
// the fallback. Keep this list and the role targets in
// ui/static/bittern.css in step.
const TARGET_RULES = [
  {
    role: "navigation",
    target: 44,
    containers: ["nav", "[role='navigation']", ".site-header", ".action-links", ".page-links"],
  },
  {
    role: "action row",
    target: 44,
    containers: [
      ".form-actions",
      ".camera-actions",
      ".candidate-actions",
      ".discovery-actions",
    ],
  },
  {
    role: "button",
    target: 44,
    controls: [
      "button",
      ".button",
      "[role='button']",
      "input[type='submit']",
      "input[type='button']",
      "input[type='reset']",
    ],
  },
  {
    role: "whole-row target",
    target: 44,
    containers: ["label.candidate-row"],
  },
];

// Everything else: inline prose links and a standalone field that no action row
// or navigation owns. This is the hard floor, not an advisory target.
const FLOOR = { role: "inline", target: 24 };

const WARNING_SIZE = 44;

async function inspectControls(page) {
  const rules = TARGET_RULES.map((rule) => ({
    role: rule.role,
    target: rule.target,
    containers: rule.containers ? rule.containers.join(", ") : "",
    controls: rule.controls ? rule.controls.join(", ") : "",
  }));
  return page.evaluate(
    ({ selector, rules, floor }) => {
      const targetFor = (element) => {
        for (const rule of rules) {
          if (rule.controls && element.matches(rule.controls)) {
            return rule;
          }
          if (rule.containers && element.closest(rule.containers)) {
            return rule;
          }
        }
        return floor;
      };
      // A wrapping label is the control's hit area: clicking anywhere on it
      // activates the control, so a 24px radio in a 44px row is a 44px target.
      const hitAreaFor = (element) => {
        const label = element.closest("label");
        if (!label) {
          return element;
        }
        const bound =
          label.control === element ||
          (element.id !== "" && label.getAttribute("for") === element.id);
        return bound ? label : element;
      };
      const describe = (element) => {
        const tag = element.tagName.toLowerCase();
        const id = element.id ? `#${element.id}` : "";
        const name = element.getAttribute("name");
        const namePart = name ? `[name='${name}']` : "";
        const label =
          (element.getAttribute("aria-label") || element.innerText || element.value || "")
            .trim()
            .replace(/\s+/g, " ")
            .slice(0, 48);
        return `${tag}${id}${namePart}${label ? ` "${label}"` : ""}`;
      };
      const viewportWidth = window.innerWidth;
      const viewportHeight = window.innerHeight;
      const scrollWidth = document.documentElement.scrollWidth;
      const controls = [];
      for (const element of document.querySelectorAll(selector)) {
        if (element.disabled || element.closest("[inert]")) {
          continue;
        }
        const style = getComputedStyle(element);
        if (
          style.display === "none" ||
          style.visibility === "hidden" ||
          style.visibility === "collapse" ||
          element.getClientRects().length === 0
        ) {
          continue;
        }

        const role = targetFor(element);
        const hitArea = hitAreaFor(element);

        const inside = (rect) =>
          rect.left >= -1 &&
          rect.top >= -1 &&
          rect.right <= viewportWidth + 1 &&
          rect.bottom <= viewportHeight + 1;

        element.scrollIntoView({
          block: "center",
          inline: "center",
          behavior: "instant",
        });
        let rect = element.getBoundingClientRect();
        const initiallyInside = inside(rect);
        const controlWidth = rect.width;
        const controlHeight = rect.height;

        let focusable = true;
        try {
          element.focus({ preventScroll: true });
          focusable = document.activeElement === element;
        } catch {
          focusable = false;
        }
        // A skip link is deliberately off-screen until it receives focus. Use
        // its focused box when focus brings it into view; an off-screen control
        // that does not appear on focus still fails below.
        const focusedRect = element.getBoundingClientRect();
        if (!initiallyInside && inside(focusedRect)) {
          rect = focusedRect;
        }
        const centreX = rect.left + rect.width / 2;
        const centreY = rect.top + rect.height / 2;
        const topmost = document.elementFromPoint(centreX, centreY);
        const hitRect = hitArea === element ? rect : hitArea.getBoundingClientRect();

        controls.push({
          description: describe(element),
          role: role.role,
          target: role.target,
          width: hitRect.width,
          height: hitRect.height,
          controlWidth,
          controlHeight,
          left: rect.left,
          top: rect.top,
          right: rect.right,
          bottom: rect.bottom,
          inside: inside(rect),
          topmost: topmost === element || element.contains(topmost),
          topmostDescription: topmost ? describe(topmost) : "nothing",
          focusable,
        });
        if (document.activeElement === element) {
          element.blur();
        }
      }
      window.scrollTo({ top: 0, left: 0, behavior: "instant" });
      return {
        scrollWidth,
        viewportWidth,
        viewportHeight,
        controls,
      };
    },
    { selector: CONTROL_SELECTOR, rules, floor: FLOOR },
  );
}

function describeSize(control) {
  const size = `${Math.round(control.width)}x${Math.round(control.height)}`;
  const controlSize = `${Math.round(control.controlWidth)}x${Math.round(control.controlHeight)}`;
  return size === controlSize
    ? size
    : `${size} on its label (control ${controlSize})`;
}

async function inspectImages(page) {
  return page.evaluate((selector) => {
    const describe = (element) => {
      const label = (element.getAttribute("alt") || element.getAttribute("src") || "")
        .trim()
        .replace(/\s+/g, " ")
        .slice(0, 48);
      return `${element.tagName.toLowerCase()}${label ? ` "${label}"` : ""}`;
    };
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const images = [];
    for (const element of document.querySelectorAll(selector)) {
      if (element.closest("[inert]")) {
        continue;
      }
      const style = getComputedStyle(element);
      if (
        style.display === "none" ||
        style.visibility === "hidden" ||
        style.visibility === "collapse" ||
        element.getClientRects().length === 0
      ) {
        continue;
      }
      element.scrollIntoView({
        block: "center",
        inline: "center",
        behavior: "instant",
      });
      const rect = element.getBoundingClientRect();
      images.push({
        description: describe(element),
        width: rect.width,
        height: rect.height,
        naturalWidth: element.naturalWidth,
        naturalHeight: element.naturalHeight,
        left: rect.left,
        top: rect.top,
        right: rect.right,
        bottom: rect.bottom,
        inside:
          rect.left >= -1 &&
          rect.top >= -1 &&
          rect.right <= viewportWidth + 1 &&
          rect.bottom <= viewportHeight + 1,
      });
    }
    window.scrollTo({ top: 0, left: 0, behavior: "instant" });
    return { viewportWidth, viewportHeight, images };
  }, IMAGE_SELECTOR);
}


async function inspectShell(page) {
  return page.evaluate(() => {
    const wrap = document.querySelector("main.wrap");
    const prose = Array.from(
      document.querySelectorAll(
        "main .lede, main .hint, main p:not(.preview-notice):not(.action-links)",
      ),
    )
      .filter((element) => {
        const style = getComputedStyle(element);
        return style.display !== "none" && element.getClientRects().length > 0;
      })
      .map((element) => {
        const style = getComputedStyle(element);
        const raw = style.maxInlineSize || style.maxWidth;
        const max = raw && raw.endsWith("px") ? parseFloat(raw) : NaN;
        return {
          text: (element.textContent || "").trim().replace(/\s+/g, " ").slice(0, 48),
          width: element.getBoundingClientRect().width,
          max: Number.isFinite(max) ? max : null,
        };
      });
    return {
      viewportWidth: window.innerWidth,
      wrapWidth: wrap ? wrap.getBoundingClientRect().width : 0,
      prose,
    };
  });
}

export async function checkLayout(page) {
  const layout = await inspectControls(page);
  const images = await inspectImages(page);
  const shell = await inspectShell(page);
  const failures = [];
  const warnings = [];

  if (layout.scrollWidth > layout.viewportWidth + 1) {
    failures.push(
      `page scrollWidth ${layout.scrollWidth} exceeds viewport width ` +
        `${layout.viewportWidth} by ${layout.scrollWidth - layout.viewportWidth}px`,
    );
  }
  if (shell.wrapWidth > 0 && shell.wrapWidth < shell.viewportWidth - 1) {
    failures.push(
      `page shell is ${Math.round(shell.wrapWidth)}px wide in a ` +
        `${shell.viewportWidth}px viewport; it must fill the width with only the gutter inset`,
    );
  }
  if (shell.viewportWidth >= 1280) {
    for (const block of shell.prose) {
      if (block.max !== null && block.width > block.max + 1) {
        failures.push(
          `prose block "${block.text}" is ${Math.round(block.width)}px wide, ` +
            `above its ${Math.round(block.max)}px reading measure`,
        );
      }
    }
  }

  for (const control of layout.controls) {
    const size = describeSize(control);
    const target = `${control.target}x${control.target}`;
    if (!control.inside) {
      failures.push(
        `${control.description} is outside the viewport after scrolling to it ` +
          `(box ${Math.round(control.left)},${Math.round(control.top)} to ` +
          `${Math.round(control.right)},${Math.round(control.bottom)} in ` +
          `${layout.viewportWidth}x${layout.viewportHeight})`,
      );
    }
    if (!control.topmost) {
      failures.push(
        `${control.description} centre is covered by ${control.topmostDescription}`,
      );
    }
    if (!control.focusable) {
      failures.push(`${control.description} cannot be focused`);
    }
    if (control.width < control.target || control.height < control.target) {
      const consequence =
        control.target > FLOOR.target
          ? `${target} ${control.role} target`
          : `${target} ${control.role} floor`;
      failures.push(`${control.description} is ${size}, below the ${consequence}`);
    } else if (control.width < WARNING_SIZE || control.height < WARNING_SIZE) {
      warnings.push(
        `${control.description} is ${size}, below the ${WARNING_SIZE}x${WARNING_SIZE} ` +
          `advisory target for ${control.role} controls; the enforced floor is ${target}`,
      );
    }
  }

  for (const image of images.images) {
    const size = `${Math.round(image.width)}x${Math.round(image.height)}`;
    if (image.naturalWidth <= 0 || image.naturalHeight <= 0) {
      failures.push(
        `${image.description} is visible but not decoded ` +
          `(natural ${image.naturalWidth}x${image.naturalHeight})`,
      );
    }
    if (image.width <= 0 || image.height <= 0) {
      failures.push(`${image.description} has no rendered box (${size})`);
    }
    if (!image.inside) {
      failures.push(
        `${image.description} is outside the viewport after scrolling to it ` +
          `(box ${Math.round(image.left)},${Math.round(image.top)} to ` +
          `${Math.round(image.right)},${Math.round(image.bottom)} in ` +
          `${images.viewportWidth}x${images.viewportHeight})`,
      );
    }
  }

  return {
    failures,
    warnings,
    controlCount: layout.controls.length,
    imageCount: images.images.length,
  };
}
