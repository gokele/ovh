/**
 * English counterpart of vps-dialogs.zh.ts (VPS control components).
 * Shape is enforced against the zh pack via VpsDialogsPack — a missing or extra
 * key fails `tsc`. OVH terminology follows OVHcloud's English docs:
 * snapshot, revert, Veeam backup, mitigation states creationPending /
 * removalPending, netboot, reinstallation.
 *
 * Critical semantics that must survive translation: every "failed to load"
 * message is NOT the same as "none exist / no IP" — keep the full warning
 * sentences intact.
 */
import type { VpsDialogsPack } from "./vps-dialogs.zh";

const pack: VpsDialogsPack = {
  vps: {
    reinstall: {
      title: "Reinstall system",
      currentOs: "Current OS",
      wipeTitle: "All data will be wiped",
      wipeDesc:
        "The data currently on this VPS disk cannot be recovered (unless you have a snapshot). Consider creating a snapshot first.",
      tplLabel: "OS template",
      loadingTemplates: "Loading templates…",
      tplLoadFailed: "Failed to load the template list: ",
      tplEmpty: "No templates available (your account/region may have no OS templates)",
      tplPlaceholder: "Pick an OS template",
      partialWhat: "OS templates",
      tplId: "Template ID: {{id}}",
      tplLocale: "default language: {{locale}}",
      tplLangs: "supports {{n}} languages",
      sshLabel: "SSH public keys (optional)",
      sshPlaceholder: "OVH SSH key names, comma-separated (taken from /me/sshKey)",
      sshHint:
        "With SSH keys filled in, you can tick \"don't email the password\" below and log in with the key right after installation",
      noPwdMail: "Don't email the initial password (log in with an SSH key)",
      confirmNameLabel: "Type the VPS name <code>{{name}}</code> to confirm:",
      submitting: "Submitting…",
      submitConfirm: "Confirm reinstall",
      toast: {
        selectTemplate: "Select an OS template first",
        nameMismatch: "VPS name does not match; cannot confirm",
        submitted: "Reinstallation task submitted; usually completes in 5-10 minutes",
      },
    },
    tasks: {
      title: "Task history",
      desc: "The last 10 tasks (reboot / reinstall / snapshot / password change etc.) with live status",
      loadFailed: "Failed to load task history",
      empty: "No task history",
      state: {
        blocked: "Blocked",
        cancelled: "Cancelled",
        doing: "In progress",
        done: "Done",
        error: "Failed",
        paused: "Paused",
        todo: "Queued",
        waitingack: "Waiting for ack",
      },
      type: {
        addVeeamBackupJob: "Add Veeam backup",
        changeRootPassword: "Reset root password",
        createSnapshot: "Create snapshot",
        deleteSnapshot: "Delete snapshot",
        deliverVm: "Deliver VM",
        getConsoleUrl: "Generate console URL",
        internalTask: "Internal task",
        migrate: "Migrate",
        openConsoleAccess: "Open console",
        provisioningAdditionalIp: "Provision additional IP",
        reOpenVm: "Power on again",
        rebootVm: "Reboot",
        reinstallVm: "Reinstall system",
        removeVeeamBackup: "Remove Veeam backup",
        rescheduleAutoBackup: "Reschedule auto backup",
        restoreFullVeeamBackup: "Veeam full restore",
        restoreVeeamBackup: "Veeam restore",
        restoreVm: "Restore VM",
        revertSnapshot: "Revert snapshot",
        setMonitoring: "Set monitoring",
        setNetboot: "Set netboot",
        startVm: "Start",
        stopVm: "Stop",
        upgradeVm: "Upgrade VM",
      },
    },
    snapshot: {
      loadFailed: "Failed to load snapshot info",
      empty: "No snapshot",
      emptyDesc:
        "The OVH free tier keeps only 1 snapshot per VPS at a time. Take one before big changes so you can revert within a minute if something goes wrong",
      createTitle: "Create snapshot",
      createDesc:
        "OVH pauses the VPS for 30 seconds to 3 minutes to take the snapshot; the network is interrupted meanwhile",
      createPlaceholder: "Description (optional), e.g. before installing nginx",
      creating: "Creating…",
      createBtn: "Create snapshot",
      currentTitle: "Current snapshot",
      creationDate: "Created: {{time}}",
      region: "Region: {{region}}",
      editDescBtn: "Edit description",
      revertBtn: "Revert to this snapshot",
      deleteBtn: "Delete snapshot",
      singleWarn:
        "The free tier allows only 1 snapshot per VPS. Delete the old one before taking a new one.",
      editTitle: "Edit snapshot description",
      descPlaceholder: "A description to identify the snapshot later",
      saving: "Saving…",
      revertTitle: "Revert to this snapshot?",
      revertDesc: "This action cannot be undone",
      revertWarnTitle: "All changes made after the snapshot will be lost:",
      revertItemFs: "The filesystem goes back to the moment of {{time}}",
      revertItemReboot: "The VPS reboots automatically and stays unreachable for a few minutes",
      revertItemMeta: "IP / password and other metadata stay unchanged",
      confirmNameLabel: "Type the VPS name <code>{{name}}</code> to confirm:",
      reverting: "Reverting…",
      revertConfirmBtn: "Confirm revert",
      deleteConfirm:
        "Delete the current snapshot? The snapshot itself is deleted; the current VPS state is unaffected.",
      toast: {
        created: "Snapshot creation task submitted; usually completes in 1-3 minutes",
        nameMismatch: "VPS name does not match",
        reverted: "Revert task submitted; the VPS is entering maintenance",
        deleted: "Snapshot deleted",
        descUpdated: "Snapshot description updated",
      },
    },
    mitigation: {
      loadFailed: "Failed to load DDoS mitigation info",
      noIp: "This VPS has no IP",
      intro:
        "OVH automatic mitigation (auto) is on by default and engages when an attack is detected. Below is the manual switch for \"permanent mitigation\" — once enabled, all VPS traffic goes through the Anti-DDoS appliances continuously (slightly higher latency, permanent protection).",
      ipv6Note:
        "IPv4 only. IPv6 is immune via the OVH network layer by default; no manual configuration needed.",
      v6NotApplicable: "anti-DDoS mitigation does not apply to IPv6 (immune at the OVH network layer)",
      noPermanent: "No permanent mitigation; automatic mitigation on standby",
      enableBtn: "Enable permanent mitigation",
      state: {
        ok: "Active",
        creationPending: "Applying",
        removalPending: "Removing",
      },
      autoTag: "auto",
      permanentTag: "permanent",
      creatingTitle:
        "Enablement in progress, usually 30 seconds to 2 minutes; wait until the state is ok before clicking disable",
      removingTitle: "Removal in progress; it will disappear from the list shortly",
      applying: "Applying…",
      removing: "Removing…",
      disableBtn: "Disable permanent",
      toast: {
        enabled: "Permanent DDoS mitigation enabled",
        disabled: "Permanent DDoS mitigation disabled",
        stateNotOk:
          "The current mitigation state does not allow disabling (it may be auto-enabling or under attack). Wait until the state is ok and retry",
        ipv4Only:
          "OVH anti-DDoS supports IPv4 only. IPv6 has network-layer protection by default; no manual configuration needed",
        failed: "Operation failed",
      },
    },
  },
};

export const vpsDialogsEn = pack;
