#include <stdint.h>
#include <stdlib.h>
uintptr_t ras_camera_create(uint64_t token);
void ras_camera_active(uintptr_t view, int active);
void ras_camera_retry(uintptr_t view);
void ras_camera_close(uintptr_t view);
char *ras_device_name(void);
