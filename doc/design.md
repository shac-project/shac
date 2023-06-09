# Shac Design

Why do we need another system for static code analysis? Shac does a few things that no known existing system does.

At a high level, shac's goals are simple:

 * Run checks safely
 * _really fast_.

By "fast" we mean "maximize utilization of available resources to minimize wall-clock delay for the user to get useful information."

These goals inform the chosen design, which in turn produces various constraints. First, the design:

 * Use a multi-pass system to determine which checks to run
 * Run checks in parallel
 * Use nsjail to sandbox checks

The multi-pass system allows shac to determine which checks to run quickly. Then shac spawns threads to actually do the work of the checks.

For each check shac provides a "passthrough" object that gives the check the ability to cache data. This helps well-written checks to continue to be fast by caching results of repetitive work. This passthrough also controls access to any external I/O like network calls.
