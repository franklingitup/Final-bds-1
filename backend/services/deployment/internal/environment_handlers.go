package deployment

import (
	"github.com/gofiber/fiber/v2"
)

func environmentID(c *fiber.Ctx) string { return c.Params("environmentId") }
func promotionID(c *fiber.Ctx) string   { return c.Params("promotionId") }

// ----------------------------------------------------------------------------
// Environment handlers
// ----------------------------------------------------------------------------

func (h *Handler) CreateEnvironment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	app := appID(c)
	if app == "" {
		return errAppNotFound
	}
	req, err := parseBody[CreateEnvironmentRequest](c)
	if err != nil {
		return err
	}
	env, err := h.svc.CreateEnvironment(c.UserContext(), org, callerIdentity(c).UserID, app, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toEnvironmentView(env))
}

func (h *Handler) GetEnvironment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	env, err := h.svc.GetEnvironment(c.UserContext(), org, callerIdentity(c).UserID, environmentID(c))
	if err != nil {
		return err
	}
	return c.JSON(toEnvironmentView(env))
}

func (h *Handler) ListEnvironments(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	app := appID(c)
	if app == "" {
		return errAppNotFound
	}
	items, err := h.svc.ListEnvironments(c.UserContext(), org, callerIdentity(c).UserID, app)
	if err != nil {
		return err
	}
	views := make([]EnvironmentView, 0, len(items))
	for _, e := range items {
		views = append(views, toEnvironmentView(&e))
	}
	return c.JSON(fiber.Map{"items": views})
}

func (h *Handler) UpdateEnvironment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[UpdateEnvironmentRequest](c)
	if err != nil {
		return err
	}
	env, err := h.svc.UpdateEnvironment(c.UserContext(), org, callerIdentity(c).UserID, environmentID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toEnvironmentView(env))
}

func (h *Handler) DeleteEnvironment(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	if err := h.svc.DeleteEnvironment(c.UserContext(), org, callerIdentity(c).UserID, environmentID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Promotion handlers
// ----------------------------------------------------------------------------

func (h *Handler) RequestPromotion(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[RequestPromotionRequest](c)
	if err != nil {
		return err
	}
	promo, err := h.svc.RequestPromotion(c.UserContext(), org, callerIdentity(c).UserID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toPromotionView(promo))
}

func (h *Handler) GetPromotion(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	promo, err := h.svc.GetPromotion(c.UserContext(), org, callerIdentity(c).UserID, promotionID(c))
	if err != nil {
		return err
	}
	return c.JSON(toPromotionView(promo))
}

func (h *Handler) ListPromotions(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	app := appID(c)
	if app == "" {
		return errAppNotFound
	}
	items, err := h.svc.ListPromotions(c.UserContext(), org, callerIdentity(c).UserID, app)
	if err != nil {
		return err
	}
	views := make([]PromotionRequestView, 0, len(items))
	for _, p := range items {
		views = append(views, toPromotionView(&p))
	}
	return c.JSON(fiber.Map{"items": views})
}

func (h *Handler) ApprovePromotion(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	promo, dep, rel, err := h.svc.ApprovePromotion(c.UserContext(), org, callerIdentity(c).UserID, promotionID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"promotion":  toPromotionView(promo),
		"deployment": toDeploymentView(dep, &rel.Revision),
		"release":    toReleaseView(rel),
	})
}

func (h *Handler) RejectPromotion(c *fiber.Ctx) error {
	org := orgID(c)
	if org == "" {
		return errOrgRequired
	}
	req, err := parseBody[RejectPromotionRequest](c)
	if err != nil {
		return err
	}
	promo, err := h.svc.RejectPromotion(c.UserContext(), org, callerIdentity(c).UserID, promotionID(c), req)
	if err != nil {
		return err
	}
	return c.JSON(toPromotionView(promo))
}
